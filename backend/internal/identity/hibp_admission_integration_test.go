//go:build integration

package identity

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

func productionHIBPTestSource(
	t *testing.T,
	respond http.HandlerFunc,
) CompromisedRangeSource {
	t.Helper()
	server := httptest.NewTLSServer(respond)
	t.Cleanup(server.Close)
	source, err := newHIBPCompromisedSource(server.URL+"/range", server.Client())
	if err != nil {
		t.Fatalf("constructing HIBP integration source: %v", err)
	}
	bounded, err := NewTimeoutCompromisedSource(source, HIBPDefaultRequestTimeout)
	if err != nil {
		t.Fatalf("bounding HIBP integration source: %v", err)
	}
	return bounded
}

func assertNoRegistrationFacts(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	var facts int
	err := pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM accounts) +
		(SELECT count(*) FROM password_credentials) +
		(SELECT count(*) FROM policy_acceptances) +
		(SELECT count(*) FROM identity_action_secrets) +
		(SELECT count(*) FROM identity_security_events) +
		(SELECT count(*) FROM outbox_events) +
		(SELECT count(*) FROM outbox_protected_payloads)`).Scan(&facts)
	if err != nil {
		t.Fatalf("counting registration facts: %v", err)
	}
	if facts != 0 {
		t.Fatalf("rejected registration created %d facts, want zero", facts)
	}
}

func approvedRegistrationService(
	t *testing.T,
	pool *pgxpool.Pool,
	randomByte byte,
	compromised CompromisedRangeSource,
) *AdmissionService {
	t.Helper()
	resolver, err := NewApprovedPolicySetResolver("https://gradex.example", ApprovedPolicySetID)
	if err != nil {
		t.Fatalf("constructing approved policy resolver: %v", err)
	}
	return admissionServiceWithResolver(
		t, pool, time.Now().UTC(), randomByte, compromised, resolver,
	)
}

func approvedStudentRegistration() StudentRegistration {
	registration := studentRegistration()
	registration.PolicySetID = ApprovedPolicySetID
	return registration
}

func TestProductionHIBPRegistrationAcceptsValidUncompromisedPassword(t *testing.T) {
	pool := admissionPool(t)
	registration := approvedStudentRegistration()
	password := registration.Password.Expose()
	fullDigest := sha1.Sum([]byte(password)) // #nosec G505 -- provider protocol fixture
	encoded := strings.ToUpper(hex.EncodeToString(fullDigest[:]))

	source := productionHIBPTestSource(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/range/"+encoded[:hibpPrefixLength] {
			t.Errorf("lookup path = %q, want exact submitted-password prefix", r.URL.EscapedPath())
		}
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "%s:0\n%s:17\n", encoded[hibpPrefixLength:], strings.Repeat("F", hibpSuffixLength))
	})
	service := approvedRegistrationService(t, pool, 0x61, source)

	if _, err := service.RegisterStudent(context.Background(), registration); err != nil {
		t.Fatalf("production-boundary registration failed: %v", err)
	}
	var accounts, credentials int
	if err := pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM accounts),
		(SELECT count(*) FROM password_credentials)`).Scan(&accounts, &credentials); err != nil {
		t.Fatalf("counting registered identity: %v", err)
	}
	if accounts != 1 || credentials != 1 {
		t.Fatalf("registered identity = %d accounts/%d credentials, want 1/1", accounts, credentials)
	}
	assertAdmissionCanariesAbsent(t, pool, password, encoded)
}

func TestProductionHIBPRegistrationRejectionsCreateNoFacts(t *testing.T) {
	tests := map[string]struct {
		password      string
		respond       func(string) http.HandlerFunc
		want          error
		wantRequests  int32
		providerToken string
	}{
		"policy invalid": {
			password: "too short",
			respond: func(_ string) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					t.Error("policy-invalid password reached the provider")
					w.WriteHeader(http.StatusInternalServerError)
				}
			},
			want: ErrPasswordPolicy,
		},
		"compromised": {
			password: "credential compromised in provider 73",
			respond: func(suffix string) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "text/plain")
					fmt.Fprintf(w, "%s:42\n", suffix)
				}
			},
			want:         ErrPasswordPolicy,
			wantRequests: 1,
		},
		"provider unavailable": {
			password: "provider outage credential 91",
			respond: func(_ string) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = w.Write([]byte("provider-internal-canary"))
				}
			},
			want:          ErrAdmissionUnavailable,
			wantRequests:  1,
			providerToken: "provider-internal-canary",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			pool := admissionPool(t)
			fullDigest := sha1.Sum([]byte(tt.password)) // #nosec G505 -- provider protocol fixture
			encoded := strings.ToUpper(hex.EncodeToString(fullDigest[:]))
			var requests atomic.Int32
			source := productionHIBPTestSource(t, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				tt.respond(encoded[hibpPrefixLength:])(w, r)
			})
			service := approvedRegistrationService(t, pool, 0x71, source)
			registration := approvedStudentRegistration()
			registration.Password = config.NewSecret(tt.password)

			_, err := service.RegisterStudent(context.Background(), registration)
			if !errors.Is(err, tt.want) {
				t.Fatalf("registration error = %v, want %v", err, tt.want)
			}
			if requests.Load() != tt.wantRequests {
				t.Fatalf("provider requests = %d, want %d", requests.Load(), tt.wantRequests)
			}
			assertNoRegistrationFacts(t, pool)
			for _, forbidden := range []string{tt.password, encoded, tt.providerToken} {
				if forbidden != "" && strings.Contains(err.Error(), forbidden) {
					t.Errorf("safe registration error exposed credential/provider material")
				}
			}
		})
	}
}
