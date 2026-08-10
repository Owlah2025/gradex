package media

import (
	"context"
	"fmt"
	"strings"
)

// DevelopmentScannerIdentity is the scanner identity persisted with every
// observation this stand-in produces. It is deliberately unmistakable in scan
// evidence: a reviewer reading `scan_attempts` can tell at a glance that no
// malware inspection happened for that Asset Version.
const DevelopmentScannerIdentity = "development-no-op-scanner"

// DevelopmentScanner is the explicit non-production stand-in that lets a
// developer or an automated acceptance run exercise the complete
// upload -> scan -> transcode -> READY path before LG-014 selects a real
// scanner.
//
// It inspects nothing. Every object version it is handed is reported PASSED,
// so it is a scanner-shaped hole, not a scanner, and it can never satisfy
// LG-014. NewDevelopmentScanner therefore refuses to construct it outside a
// development environment, and config.validate refuses the mode that selects
// it anywhere else.
type DevelopmentScanner struct{}

// NewDevelopmentScanner builds the stand-in only for APP_ENV=development. Any
// other environment is a construction error rather than a silent downgrade to
// an unscanned pipeline.
func NewDevelopmentScanner(appEnv string) (*DevelopmentScanner, error) {
	if !strings.EqualFold(strings.TrimSpace(appEnv), "development") {
		return nil, fmt.Errorf(
			"%w: the development no-op scanner is refused outside APP_ENV=development (got %q)",
			ErrScannerMisconfigured, appEnv,
		)
	}
	return &DevelopmentScanner{}, nil
}

// NewConfiguredScanner builds the scanner boundary named by MEDIA_SCANNER_MODE.
// An unknown mode is a startup error: a process must never fall back to an
// unscanned pipeline because its configuration was unreadable.
func NewConfiguredScanner(scannerMode, appEnv string) (Scanner, error) {
	switch strings.ToUpper(strings.TrimSpace(scannerMode)) {
	case "DEVELOPMENT_NO_OP":
		scanner, err := NewDevelopmentScanner(appEnv)
		if err != nil {
			return nil, err
		}
		return scanner, nil
	case "", "UNAVAILABLE":
		scanner, err := NewUnavailableScanner("LG-014 scanner is not configured")
		if err != nil {
			return nil, err
		}
		return scanner, nil
	default:
		return nil, fmt.Errorf("%w: unknown media scanner mode %q", ErrScannerMisconfigured, scannerMode)
	}
}

func (s *DevelopmentScanner) Scan(_ context.Context, object ObjectVersion) (ScanObservation, error) {
	return ScanObservation{
		AssetVersionID:       object.AssetVersionID,
		StorageObjectVersion: object.StorageObjectVersion,
		Outcome:              ScanPassed,
		ScannerIdentity:      DevelopmentScannerIdentity,
		Reason:               "development environment: no malware inspection was performed",
	}, nil
}
