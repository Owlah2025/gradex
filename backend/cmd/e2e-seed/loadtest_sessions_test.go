//go:build !production

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/identity"
)

type loadtestSession struct {
	Index       int    `json:"index"`
	AccountID   string `json:"account_id"`
	Email       string `json:"email"`
	CookieName  string `json:"cookie_name"`
	CookieValue string `json:"cookie_value"`
	CSRFToken   string `json:"csrf_token"`
}

type loadtestSessionManifest struct {
	SchemaVersion int               `json:"schema_version"`
	Sessions      []loadtestSession `json:"sessions"`
}

func writeLoadtestSessionManifest(path string, manifest loadtestSessionManifest) error {
	if path == "" {
		return fmt.Errorf("load-test session output path is required")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	if err := encoder.Encode(manifest); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

// issueLoadtestSessions performs the same password verification and session-family write as the
// HTTP login handler, but outside the measured API/playback scenarios. The resulting credentials
// are production-valid and are accepted by the ordinary authentication and entitlement middleware.
func issueLoadtestSessions(ctx context.Context, targetDSN, password string) (loadtestSessionManifest, error) {
	result := loadtestSessionManifest{SchemaVersion: 1}
	if password == "" {
		return result, fmt.Errorf("load-test password is required")
	}
	cfg, err := config.Load()
	if err != nil {
		return result, fmt.Errorf("loading production configuration: %w", err)
	}
	if !cfg.Sessions().Enabled() {
		return result, fmt.Errorf("session settings are disabled")
	}
	pool, err := pgxpool.New(ctx, targetDSN)
	if err != nil {
		return result, fmt.Errorf("connecting to load-test database: %w", err)
	}
	defer pool.Close()
	repository, err := identity.NewSessionRepository(identity.SessionRepositoryOptions{
		Pool: pool, Settings: cfg.Sessions(), CSRFKey: []byte(cfg.Sessions().CSRFKey().Expose()), Now: time.Now,
	})
	if err != nil {
		return result, fmt.Errorf("building session repository: %w", err)
	}

	result.Sessions = make([]loadtestSession, 0, loadtestActiveStudentCount)
	for index := 0; index < loadtestActiveStudentCount; index++ {
		email := loadtestStudentEmail(index)
		grant, err := repository.Login(ctx, identity.LoginRequest{
			Email: email, Password: config.NewSecret(password),
			RequestID: fmt.Sprintf("loadtest-session-%04d", index),
		})
		if err != nil {
			return loadtestSessionManifest{}, fmt.Errorf("issuing session %d: %w", index, err)
		}
		result.Sessions = append(result.Sessions, loadtestSession{
			Index: index, AccountID: grant.Session.AccountID, Email: email,
			CookieName: auth.SessionCookieName, CookieValue: grant.Credential.Expose(),
			CSRFToken: grant.CSRFToken.Expose(),
		})
	}
	return result, nil
}

func TestWriteLoadtestSessionManifestIsExclusiveAndPrivate(t *testing.T) {
	path := t.TempDir() + "/sessions.json"
	manifest := loadtestSessionManifest{SchemaVersion: 1, Sessions: []loadtestSession{{
		Index: 0, AccountID: loadtestStudentID(0), Email: loadtestStudentEmail(0),
		CookieName: "gradex_session", CookieValue: "credential-canary", CSRFToken: "csrf-canary",
	}}}
	if err := writeLoadtestSessionManifest(path, manifest); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("session manifest mode = %o, want 600", info.Mode().Perm())
	}
	if err := writeLoadtestSessionManifest(path, manifest); err == nil {
		t.Fatal("session manifest writer overwrote an existing credential file")
	}
}
