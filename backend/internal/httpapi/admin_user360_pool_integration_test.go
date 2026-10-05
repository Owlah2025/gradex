//go:build integration

package httpapi

import (
	"context"
	"crypto/rand"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	adminread "github.com/Owlah2025/gradex/backend/internal/admin"
	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/identity"
)

type barrierDeviceReader struct {
	devices *identity.DeviceService
	entered chan struct{}
	release chan struct{}
}

func (r barrierDeviceReader) AdminOverviewInTransaction(ctx context.Context, tx pgx.Tx, accountID string, now time.Time) (identity.AdminDeviceOverview, error) {
	r.entered <- struct{}{}
	select {
	case <-r.release:
		return r.devices.AdminOverviewInTransaction(ctx, tx, accountID, now)
	case <-ctx.Done():
		return identity.AdminDeviceOverview{}, ctx.Err()
	}
}

func TestAdminUser360UsesOneConnectionAndAuditsConcurrentReads(t *testing.T) {
	_, fixturePool, adminID, _, _, _, _, _ := setupAdminPricingAPIServer(t)
	ctx := context.Background()
	studentID := uuid.NewString()
	if _, err := fixturePool.Exec(ctx, `INSERT INTO accounts(id,normalized_email,email,role,status,display_name)
		VALUES($1::uuid,'pool-student@example.com','pool-student@example.com','STUDENT','ACTIVE','Pool Student')`, studentID); err != nil {
		t.Fatal(err)
	}
	for _, revoked := range []bool{false, true} {
		var revokedAt *time.Time
		var reason *string
		if revoked {
			now, value := time.Now().UTC(), "ADMIN_REVOKED"
			revokedAt, reason = &now, &value
		}
		if _, err := fixturePool.Exec(ctx, `INSERT INTO identity_trusted_devices(id,account_id,label,browser_family,platform_family,credential_digest,first_seen_at,trusted_at,revoked_at,revocation_reason)
			VALUES($1::uuid,$2::uuid,'Browser','Chrome','Linux',$3,now()-interval '1 day',now()-interval '1 hour',$4,$5)`, uuid.NewString(), studentID, uuid.NewString(), revokedAt, reason); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fixturePool.Exec(ctx, `INSERT INTO identity_device_replacement_state(account_id,last_replacement_at) VALUES($1::uuid,now())`, studentID); err != nil {
		t.Fatal(err)
	}
	principal := identity.Principal{AccountID: adminID, Role: identity.RoleAdmin, Status: identity.StatusActive, CredentialState: identity.CredentialActive}

	for _, connections := range []int32{1, 2} {
		t.Run(fmt.Sprintf("MaxConns=%d", connections), func(t *testing.T) {
			cfg := fixturePool.Config().Copy()
			cfg.MaxConns, cfg.MinConns = connections, 0
			p, err := pgxpool.NewWithConfig(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			devices, err := identity.NewDeviceService(identity.DeviceServiceOptions{
				Pool: p, Outbox: testWriterForAuthoring(t),
				Policy: identity.DevicePolicy{TrustedDeviceLimit: 2, ReplacementCooldown: 24 * time.Hour},
				Pepper: config.NewSecret("user360-local-test-pepper-32-bytes"), OTPTTL: 10 * time.Minute, Now: time.Now, Random: rand.Reader,
			})
			if err != nil {
				t.Fatal(err)
			}
			barrier := barrierDeviceReader{devices: devices, entered: make(chan struct{}, connections), release: make(chan struct{})}
			repo, err := adminread.NewRepositoryWithOptions(p, adminread.RepositoryOptions{Devices: barrier})
			if err != nil {
				t.Fatal(err)
			}
			bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			type result struct {
				view adminread.User360
				err  error
			}
			done := make(chan result, connections)
			correlation := uuid.NewString()
			for i := int32(0); i < connections; i++ {
				go func() {
					view, err := repo.GetUser360(bounded, adminread.User360Request{Principal: principal, Locale: identity.LocaleEnglish, AccountID: studentID, CorrelationID: correlation})
					done <- result{view, err}
				}()
			}
			// Every request holds its transaction before any device query proceeds.
			for i := int32(0); i < connections; i++ {
				select {
				case <-barrier.entered:
				case <-bounded.Done():
					t.Fatal("reads did not reach the barrier")
				}
			}
			close(barrier.release)
			for i := int32(0); i < connections; i++ {
				got := <-done
				if got.err != nil {
					t.Fatal(got.err)
				}
				if got.view.Student == nil {
					t.Fatal("missing Student view")
				}
				overview := got.view.Student.Devices
				if len(overview.Devices) != 2 || overview.DeviceLimit != 2 || overview.CooldownUntil == nil {
					t.Fatalf("incomplete device history: %+v", overview)
				}
				if overview.Devices[0].State != "TRUSTED" || overview.Devices[1].State != "REVOKED" {
					t.Fatalf("device states: %+v", overview.Devices)
				}
			}
			var audits int
			if err := p.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action='ADMIN_USER_VIEWED' AND correlation_id=$1`, correlation).Scan(&audits); err != nil {
				t.Fatal(err)
			}
			if audits != int(connections) {
				t.Fatalf("audits=%d want %d", audits, connections)
			}

			// An audit insertion failure must return no privileged data or audit row.
			badActor := principal
			badActor.AccountID = uuid.NewString()
			failedCorrelation := uuid.NewString()
			view, err := repo.GetUser360(bounded, adminread.User360Request{Principal: badActor, Locale: identity.LocaleEnglish, AccountID: studentID, CorrelationID: failedCorrelation})
			if err == nil || view.Student != nil {
				t.Fatalf("audit failure returned data: %+v, %v", view, err)
			}
			if err := p.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE correlation_id=$1`, failedCorrelation).Scan(&audits); err != nil {
				t.Fatal(err)
			}
			if audits != 0 {
				t.Fatalf("failed read committed %d audit rows", audits)
			}
		})
	}
}
