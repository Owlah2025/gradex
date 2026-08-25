//go:build !production

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

type betaLoadtestStudentSession struct {
	Index          int    `json:"index"`
	AccountID      string `json:"account_id"`
	Email          string `json:"email"`
	Entitled       bool   `json:"entitled"`
	CourseID       string `json:"course_id,omitempty"`
	RevisionID     string `json:"revision_id,omitempty"`
	LessonID       string `json:"lesson_id,omitempty"`
	AssetVersionID string `json:"asset_version_id,omitempty"`
	CookieName     string `json:"cookie_name"`
	CookieValue    string `json:"cookie_value"`
	CSRFToken      string `json:"csrf_token"`
}

type betaLoadtestOperatorSession struct {
	Role        string   `json:"role"`
	Index       int      `json:"index"`
	AccountID   string   `json:"account_id"`
	Email       string   `json:"email"`
	CourseIDs   []string `json:"course_ids"`
	CookieName  string   `json:"cookie_name"`
	CookieValue string   `json:"cookie_value"`
	CSRFToken   string   `json:"csrf_token"`
}

type betaLoadtestSessionManifest struct {
	SchemaVersion int                           `json:"schema_version"`
	Profile       string                        `json:"profile"`
	RunID         string                        `json:"run_id"`
	Students      []betaLoadtestStudentSession  `json:"students"`
	Operators     []betaLoadtestOperatorSession `json:"operators"`
}

func issueBetaLoadtestSessions(ctx context.Context, targetDSN, password, fixturePath, outputPath string) error {
	if password == "" || fixturePath == "" || outputPath == "" {
		return fmt.Errorf("beta session issuance requires password, fixture path, and output path")
	}
	fixtureBytes, err := os.ReadFile(fixturePath)
	if err != nil {
		return fmt.Errorf("reading beta fixture manifest: %w", err)
	}
	var fixture betaFixtureManifest
	if err := json.Unmarshal(fixtureBytes, &fixture); err != nil {
		return fmt.Errorf("decoding beta fixture manifest: %w", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading production configuration: %w", err)
	}
	if !cfg.Sessions().Enabled() {
		return fmt.Errorf("session settings are disabled")
	}
	pool, err := pgxpool.New(ctx, targetDSN)
	if err != nil {
		return fmt.Errorf("connecting to beta fixture database: %w", err)
	}
	defer pool.Close()
	repository, err := identity.NewSessionRepository(identity.SessionRepositoryOptions{
		Pool: pool, Settings: cfg.Sessions(), CSRFKey: []byte(cfg.Sessions().CSRFKey().Expose()), Now: time.Now,
	})
	if err != nil {
		return fmt.Errorf("building beta session repository: %w", err)
	}
	manifest := betaLoadtestSessionManifest{SchemaVersion: betaFixtureSchemaVersion, Profile: fixture.Profile, RunID: fixture.RunID}
	manifest.Students = make([]betaLoadtestStudentSession, 0, len(fixture.Students))
	for _, student := range fixture.Students {
		grant, err := repository.Login(ctx, identity.LoginRequest{
			Email: student.Email, Password: config.NewSecret(password), RequestID: fmt.Sprintf("%s-beta-student-%03d", fixture.RunID, student.Index),
		})
		if err != nil {
			return fmt.Errorf("issuing beta Student session %d: %w", student.Index, err)
		}
		manifest.Students = append(manifest.Students, betaLoadtestStudentSession{
			Index: student.Index, AccountID: student.AccountID, Email: student.Email, Entitled: student.Entitled,
			CourseID: student.CourseID, RevisionID: student.RevisionID, LessonID: student.LessonID, AssetVersionID: student.AssetVersionID,
			CookieName: auth.SessionCookieName, CookieValue: grant.Credential.Expose(), CSRFToken: grant.CSRFToken.Expose(),
		})
	}
	manifest.Operators = make([]betaLoadtestOperatorSession, 0, len(fixture.Operators))
	for _, operator := range fixture.Operators {
		grant, err := repository.Login(ctx, identity.LoginRequest{
			Email: operator.Email, Password: config.NewSecret(password), RequestID: fmt.Sprintf("%s-beta-operator-%s-%02d", fixture.RunID, operator.Role, operator.Index),
		})
		if err != nil {
			return fmt.Errorf("issuing beta %s session %d: %w", operator.Role, operator.Index, err)
		}
		manifest.Operators = append(manifest.Operators, betaLoadtestOperatorSession{
			Role: operator.Role, Index: operator.Index, AccountID: operator.AccountID, Email: operator.Email, CourseIDs: operator.CourseIDs,
			CookieName: auth.SessionCookieName, CookieValue: grant.Credential.Expose(), CSRFToken: grant.CSRFToken.Expose(),
		})
	}
	return writeBetaLoadtestSessionManifest(outputPath, manifest)
}

func writeBetaLoadtestSessionManifest(path string, manifest betaLoadtestSessionManifest) error {
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

func TestBetaSessionManifestWriterIsExclusiveAndPrivate(t *testing.T) {
	path := t.TempDir() + "/sessions.json"
	manifest := betaLoadtestSessionManifest{SchemaVersion: betaFixtureSchemaVersion, Profile: "limited-paid-beta", RunID: "run-20260824"}
	if err := writeBetaLoadtestSessionManifest(path, manifest); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("session manifest mode = %o, want 600", info.Mode().Perm())
	}
	if err := writeBetaLoadtestSessionManifest(path, manifest); err == nil {
		t.Fatal("beta session writer overwrote an existing credential file")
	}
}
