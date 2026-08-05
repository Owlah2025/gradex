package e2esafety

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Protected database names that must never be targeted for reset.
var protectedDatabaseNames = map[string]bool{
	"postgres":          true,
	"gradex":            true,
	"gradex_dev":        true,
	"gradex_production": true,
	"production":        true,
	"master":            true,
	"main":              true,
}

// Accepted local/test hostnames for E2E execution.
var acceptedTestHosts = map[string]bool{
	"127.0.0.1":             true,
	"localhost":             true,
	"::1":                   true,
	"backend-postgres-1":    true,
	"postgres":              true,
	"localhost.localdomain": true,
}

const (
	ExpectedResetAcknowledgement = "1"
	AllowedDatabasePrefix        = "gradex_playwright_e2e"
)

// SafetyConfig holds the inputs needed to validate a database reset operation.
type SafetyConfig struct {
	AdminDSN                string
	TargetDSN               string
	AppDSN                  string
	ResetAcknowledgementEnv string
}

// ValidateE2EDatabaseTarget performs strict fail-closed safety checks before any
// destructive database operation (DROP DATABASE, schema reset, or migration execution).
func ValidateE2EDatabaseTarget(cfg SafetyConfig) error {
	// 1. Destructive reset acknowledgement check
	if strings.TrimSpace(cfg.ResetAcknowledgementEnv) != ExpectedResetAcknowledgement {
		return errors.New("E2E database reset refused: GRADEX_E2E_ALLOW_DATABASE_RESET=1 environment variable is missing or invalid")
	}

	// 2. Validate Admin DSN
	if strings.TrimSpace(cfg.AdminDSN) == "" {
		return errors.New("E2E database reset refused: admin DSN is empty")
	}
	adminURL, err := url.Parse(cfg.AdminDSN)
	if err != nil {
		return fmt.Errorf("E2E database reset refused: unparseable admin DSN: %w", err)
	}

	// 3. Validate Target DSN
	if strings.TrimSpace(cfg.TargetDSN) == "" {
		return errors.New("E2E database reset refused: target DSN is empty")
	}
	targetURL, err := url.Parse(cfg.TargetDSN)
	if err != nil {
		return fmt.Errorf("E2E database reset refused: unparseable target DSN: %w", err)
	}

	// 4. Host check for local/test infrastructure
	adminHost := strings.ToLower(adminURL.Hostname())
	targetHost := strings.ToLower(targetURL.Hostname())
	if !acceptedTestHosts[adminHost] {
		return fmt.Errorf("E2E database reset refused: admin host %q is not an accepted local/test host", adminHost)
	}
	if !acceptedTestHosts[targetHost] {
		return fmt.Errorf("E2E database reset refused: target host %q is not an accepted local/test host", targetHost)
	}

	// 5. Target DB name extraction & strict name validation
	targetDBName := strings.TrimPrefix(targetURL.Path, "/")
	if err := ValidateDatabaseName(targetDBName); err != nil {
		return fmt.Errorf("E2E database reset refused: %w", err)
	}

	// 6. Check against normal application database URL
	if strings.TrimSpace(cfg.AppDSN) != "" {
		appURL, err := url.Parse(cfg.AppDSN)
		if err == nil {
			appDBName := strings.TrimPrefix(appURL.Path, "/")
			if strings.EqualFold(targetDBName, appDBName) {
				return fmt.Errorf("E2E database reset refused: target database %q equals application database name", targetDBName)
			}
			if canonicalDSN(cfg.TargetDSN) == canonicalDSN(cfg.AppDSN) {
				return errors.New("E2E database reset refused: target DSN equals regular application DSN")
			}
		}
	}

	return nil
}

var safeDBNameRegex = regexp.MustCompile(`^gradex_playwright_e2e_[a-z0-9]{8,24}$`)

// ValidateDatabaseName verifies that a database name contains only allowed ASCII
// lowercase letters, digits, and underscores, starts with gradex_playwright_e2e_,
// has a per-run suffix of 8 to 24 lowercase alphanumeric characters, is not protected,
// and total byte length is <= 63 bytes (PostgreSQL identifier limit).
func ValidateDatabaseName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("database name is empty")
	}
	if len(name) > 63 {
		return fmt.Errorf("database name %q length %d exceeds PostgreSQL 63-byte limit", name, len(name))
	}
	if protectedDatabaseNames[strings.ToLower(name)] {
		return fmt.Errorf("database name %q is a protected database name", name)
	}
	if !safeDBNameRegex.MatchString(name) {
		return fmt.Errorf("database name %q contains invalid characters or does not match safe pattern ^gradex_playwright_e2e_[a-z0-9]{8,24}$", name)
	}
	return nil
}

// QuoteIdentifier safely double-quotes a validated database identifier for PostgreSQL DDL statements.
func QuoteIdentifier(name string) (string, error) {
	if err := ValidateDatabaseName(name); err != nil {
		return "", fmt.Errorf("cannot quote unsafe database identifier: %w", err)
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`, nil
}

func canonicalDSN(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return u.Scheme + "://" + u.Host + u.Path
}
