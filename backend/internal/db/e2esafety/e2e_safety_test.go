package e2esafety

import (
	"testing"
)

func TestValidateE2EDatabaseTarget_Success(t *testing.T) {
	cfg := SafetyConfig{
		AdminDSN:                "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable",
		TargetDSN:               "postgres://gradex:gradex@localhost:5432/gradex_playwright_e2e_run12345?sslmode=disable",
		AppDSN:                  "postgres://gradex:gradex@localhost:5432/gradex?sslmode=disable",
		ResetAcknowledgementEnv: "1",
	}
	if err := ValidateE2EDatabaseTarget(cfg); err != nil {
		t.Fatalf("expected valid configuration to pass, got: %v", err)
	}
}

func TestValidateE2EDatabaseTarget_RefusesMissingAcknowledgement(t *testing.T) {
	cfg := SafetyConfig{
		AdminDSN:                "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable",
		TargetDSN:               "postgres://gradex:gradex@localhost:5432/gradex_playwright_e2e_run12345?sslmode=disable",
		AppDSN:                  "postgres://gradex:gradex@localhost:5432/gradex?sslmode=disable",
		ResetAcknowledgementEnv: "",
	}
	if err := ValidateE2EDatabaseTarget(cfg); err == nil {
		t.Fatal("expected error when reset acknowledgement is missing, got nil")
	}
}

func TestValidateE2EDatabaseTarget_RefusesProductionHostname(t *testing.T) {
	cfg := SafetyConfig{
		AdminDSN:                "postgres://gradex:gradex@db.production.example.com:5432/postgres?sslmode=disable",
		TargetDSN:               "postgres://gradex:gradex@db.production.example.com:5432/gradex_playwright_e2e_run1?sslmode=disable",
		AppDSN:                  "postgres://gradex:gradex@localhost:5432/gradex?sslmode=disable",
		ResetAcknowledgementEnv: "1",
	}
	if err := ValidateE2EDatabaseTarget(cfg); err == nil {
		t.Fatal("expected error when host is remote/production, got nil")
	}
}

func TestValidateE2EDatabaseTarget_RefusesProtectedDatabaseName(t *testing.T) {
	for _, protected := range []string{"postgres", "gradex", "gradex_dev", "gradex_production", "production"} {
		cfg := SafetyConfig{
			AdminDSN:                "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable",
			TargetDSN:               "postgres://gradex:gradex@localhost:5432/" + protected + "?sslmode=disable",
			AppDSN:                  "postgres://gradex:gradex@localhost:5432/gradex_other?sslmode=disable",
			ResetAcknowledgementEnv: "1",
		}
		if err := ValidateE2EDatabaseTarget(cfg); err == nil {
			t.Fatalf("expected error for protected db name %q, got nil", protected)
		}
	}
}

func TestValidateE2EDatabaseTarget_RefusesMatchingAppDSN(t *testing.T) {
	cfg := SafetyConfig{
		AdminDSN:                "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable",
		TargetDSN:               "postgres://gradex:gradex@localhost:5432/gradex_playwright_e2e_run1?sslmode=disable",
		AppDSN:                  "postgres://gradex:gradex@localhost:5432/gradex_playwright_e2e_run1?sslmode=disable",
		ResetAcknowledgementEnv: "1",
	}
	if err := ValidateE2EDatabaseTarget(cfg); err == nil {
		t.Fatal("expected error when target DSN matches app DSN, got nil")
	}
}

func TestValidateE2EDatabaseTarget_RefusesUnsafePrefix(t *testing.T) {
	cfg := SafetyConfig{
		AdminDSN:                "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable",
		TargetDSN:               "postgres://gradex:gradex@localhost:5432/my_custom_test_db?sslmode=disable",
		AppDSN:                  "postgres://gradex:gradex@localhost:5432/gradex?sslmode=disable",
		ResetAcknowledgementEnv: "1",
	}
	if err := ValidateE2EDatabaseTarget(cfg); err == nil {
		t.Fatal("expected error when target DB name lacks safe prefix, got nil")
	}
}

func TestValidateDatabaseName_RefusesUnsafePatterns(t *testing.T) {
	unsafeNames := []string{
		"gradex_playwright_e2e",
		"gradex_playwright_e2e_",
		"gradex_playwright_e2e_a",
		"gradex_playwright_e2e-UPPER",
		"gradex_playwright_e2e_x;DROP DATABASE postgres",
		`gradex_playwright_e2e_"bad"`,
		"gradex_playwright_e2e_../bad",
		"gradex_playwright_e2e_x'or'1'='1",
		"gradex_playwright_e2e_space name",
		"gradex_playwright_e2e_12345678901234567890123456789012345678901234567890", // > 63 bytes
	}
	for _, name := range unsafeNames {
		if err := ValidateDatabaseName(name); err == nil {
			t.Errorf("expected ValidateDatabaseName(%q) to fail, but it passed", name)
		}
		if _, err := QuoteIdentifier(name); err == nil {
			t.Errorf("expected QuoteIdentifier(%q) to fail, but it passed", name)
		}
	}
}

func TestValidateDatabaseName_AcceptsValidPatterns(t *testing.T) {
	validNames := []string{
		"gradex_playwright_e2e_12345678",
		"gradex_playwright_e2e_run12345",
		"gradex_playwright_e2e_abcdef0123456789",
	}
	for _, name := range validNames {
		if err := ValidateDatabaseName(name); err != nil {
			t.Errorf("expected ValidateDatabaseName(%q) to pass, got error: %v", name, err)
		}
		quoted, err := QuoteIdentifier(name)
		if err != nil {
			t.Errorf("expected QuoteIdentifier(%q) to pass, got error: %v", name, err)
		}
		expectedQuoted := `"` + name + `"`
		if quoted != expectedQuoted {
			t.Errorf("QuoteIdentifier(%q) = %q, expected %q", name, quoted, expectedQuoted)
		}
	}
}
