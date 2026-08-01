package media

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ScanOutcome is the only result vocabulary persisted for a scan. Absence or
// misconfiguration is an error and never a successful outcome.
type ScanOutcome string

const (
	ScanPassed ScanOutcome = "PASSED"
	ScanFailed ScanOutcome = "FAILED"
	ScanError  ScanOutcome = "ERROR"
)

var (
	ErrScannerRequired        = errors.New("scanner is required")
	ErrScannerUnavailable     = errors.New("scanner unavailable")
	ErrScannerMisconfigured   = errors.New("scanner misconfigured")
	ErrMalwareDetected        = errors.New("malware detected")
	ErrStaleScanEvidence      = errors.New("scan evidence does not match the exact object version")
	ErrInvalidScanObservation = errors.New("invalid scan observation")
)

// ObjectVersion is the immutable identity a scanner must inspect. The logical
// asset ID is intentionally absent: a logical asset can have replacement
// bytes, and a pass for one replacement must not transfer to another.
type ObjectVersion struct {
	AssetVersionID       string
	StorageObjectKey     string
	StorageObjectVersion string
}

func (o ObjectVersion) valid() bool {
	return strings.TrimSpace(o.AssetVersionID) != "" &&
		strings.TrimSpace(o.StorageObjectKey) != "" &&
		strings.TrimSpace(o.StorageObjectVersion) != ""
}

// ScanObservation is returned by an adapter and is later persisted as
// evidence. The adapter must echo the exact object identity it inspected.
type ScanObservation struct {
	AssetVersionID       string
	StorageObjectVersion string
	Outcome              ScanOutcome
	ScannerIdentity      string
	Reason               string
}

// Scanner is the provider-neutral malware-scanning boundary. No concrete
// production provider is selected while LG-014 is unresolved.
type Scanner interface {
	Scan(context.Context, ObjectVersion) (ScanObservation, error)
}

// ScannerAdapter validates the required scanner boundary at construction and
// normalizes provider observations. A nil adapter cannot be constructed.
type ScannerAdapter struct {
	scanner Scanner
}

func NewScannerAdapter(scanner Scanner) (*ScannerAdapter, error) {
	if scanner == nil {
		return nil, ErrScannerRequired
	}
	return &ScannerAdapter{scanner: scanner}, nil
}

// UnavailableScanner is an explicit non-production-capability representation.
// It is useful for a deployment where LG-014 is not configured: every scan
// remains an error and the asset stays non-deliverable.
type UnavailableScanner struct{ Reason string }

func NewUnavailableScanner(reason string) (*UnavailableScanner, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, ErrScannerMisconfigured
	}
	return &UnavailableScanner{Reason: reason}, nil
}

func (s *UnavailableScanner) Scan(context.Context, ObjectVersion) (ScanObservation, error) {
	return ScanObservation{Outcome: ScanError, Reason: s.Reason},
		fmt.Errorf("%w: %s", ErrScannerUnavailable, s.Reason)
}

// Scan sends one exact object version to the adapter. Provider errors and
// context timeouts are represented as ScanError and returned to the caller for
// diagnostics; neither can produce SCAN_PASSED.
func (a *ScannerAdapter) Scan(ctx context.Context, object ObjectVersion) (ScanObservation, error) {
	if a == nil || a.scanner == nil {
		return ScanObservation{
			AssetVersionID: object.AssetVersionID, StorageObjectVersion: object.StorageObjectVersion,
			Outcome: ScanError, Reason: ErrScannerMisconfigured.Error(),
		}, ErrScannerMisconfigured
	}
	if !object.valid() {
		return ScanObservation{Outcome: ScanError, Reason: "object identity is incomplete"}, ErrStaleScanEvidence
	}
	if err := ctx.Err(); err != nil {
		return ScanObservation{
			AssetVersionID: object.AssetVersionID, StorageObjectVersion: object.StorageObjectVersion,
			Outcome: ScanError, Reason: err.Error(),
		}, fmt.Errorf("%w: %v", ErrScannerUnavailable, err)
	}

	observation, err := a.scanner.Scan(ctx, object)
	if err != nil {
		return ScanObservation{
			AssetVersionID: object.AssetVersionID, StorageObjectVersion: object.StorageObjectVersion,
			Outcome: ScanError, Reason: err.Error(),
		}, fmt.Errorf("%w: %v", ErrScannerUnavailable, err)
	}
	if observation.AssetVersionID != object.AssetVersionID ||
		observation.StorageObjectVersion != object.StorageObjectVersion {
		return observation, ErrStaleScanEvidence
	}
	if !validScanOutcome(observation.Outcome) || strings.TrimSpace(observation.ScannerIdentity) == "" {
		return observation, ErrInvalidScanObservation
	}
	if observation.Outcome == ScanFailed && strings.TrimSpace(observation.Reason) == "" {
		observation.Reason = ErrMalwareDetected.Error()
	}
	return observation, nil
}

func validScanOutcome(outcome ScanOutcome) bool {
	switch outcome {
	case ScanPassed, ScanFailed, ScanError:
		return true
	default:
		return false
	}
}

// ApplyScanObservation accepts a result only for the exact currently scanned
// object. It returns the next state without mutating a caller-owned version.
func ApplyScanObservation(current AssetVersionState, object ObjectVersion, observation ScanObservation) (AssetVersionState, error) {
	if current != StateScanning {
		return current, fmt.Errorf("scan observation requires SCANNING, got %q", current)
	}
	if !object.valid() || observation.AssetVersionID != object.AssetVersionID ||
		observation.StorageObjectVersion != object.StorageObjectVersion {
		return current, ErrStaleScanEvidence
	}
	next, err := ScanTransition(observation.Outcome)
	if err != nil {
		return current, err
	}
	if err := Transition(current, next); err != nil {
		return current, err
	}
	return next, nil
}
