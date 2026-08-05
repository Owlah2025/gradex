//go:build !production

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/identity"
)

// Test-runner-side session issuance.
//
// Repeated runs need dozens of authenticated Students, but the login endpoint is bounded at 30
// requests per minute per source network — a production limit this suite must respect rather
// than raise. Issuing sessions here keeps every one of those limits intact while still using the
// production authentication path: the real `SessionRepository`, loaded from the real
// configuration, verifying the real Argon2id credential and writing the real session family and
// generation rows. Nothing about the session is invented — the same code the HTTP handler calls
// produces it, so production middleware accepts it exactly as it accepts a browser login.
//
// The infrastructure smoke test continues to exercise the full HTTP login flow, so the login
// route itself remains covered.
type issuedSessionOutput struct {
	AccountID string `json:"account_id"`
	// CookieName is the production cookie name; the value is the opaque session credential.
	CookieName  string `json:"cookie_name"`
	CookieValue string `json:"cookie_value"`
	// CSRFToken is what the browser would have held in memory after logging in. It never reaches
	// disk: this is written to stdout for the test runner and nothing else.
	CSRFToken string `json:"csrf_token"`
}

func issueSession(ctx context.Context, targetDSN, email, password string) (issuedSessionOutput, error) {
	// The production loader, so session windows and the CSRF key are resolved exactly as the API
	// resolves them. The same SESSION_CSRF_KEY the API runs with must be in this process's
	// environment, or the issued CSRF token would not validate.
	cfg, err := config.Load()
	if err != nil {
		return issuedSessionOutput{}, fmt.Errorf("loading production configuration: %w", err)
	}
	if !cfg.Sessions().Enabled() {
		return issuedSessionOutput{}, fmt.Errorf("session settings are disabled; SESSION_CSRF_KEY is required")
	}

	pool, err := pgxpool.New(ctx, targetDSN)
	if err != nil {
		return issuedSessionOutput{}, fmt.Errorf("connecting to target db for session issuance: %w", err)
	}
	defer pool.Close()

	repository, err := identity.NewSessionRepository(identity.SessionRepositoryOptions{
		Pool:     pool,
		Settings: cfg.Sessions(),
		CSRFKey:  []byte(cfg.Sessions().CSRFKey().Expose()),
		Now:      time.Now,
	})
	if err != nil {
		return issuedSessionOutput{}, fmt.Errorf("building session repository: %w", err)
	}

	grant, err := repository.Login(ctx, identity.LoginRequest{
		Email:     email,
		Password:  config.NewSecret(password),
		RequestID: "e2e-session-issuance",
	})
	if err != nil {
		return issuedSessionOutput{}, fmt.Errorf("issuing session for %s: %w", email, err)
	}

	return issuedSessionOutput{
		AccountID:   grant.Session.AccountID,
		CookieName:  auth.SessionCookieName,
		CookieValue: grant.Credential.Expose(),
		CSRFToken:   grant.CSRFToken.Expose(),
	}, nil
}

func encodeIssuedSession(session issuedSessionOutput) ([]byte, error) {
	encoded, err := json.Marshal(session)
	if err != nil {
		return nil, fmt.Errorf("encoding issued session: %w", err)
	}
	return encoded, nil
}
