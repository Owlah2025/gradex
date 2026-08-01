package media

import "fmt"

// AssetVersionState is the byte-processing state of one immutable Asset
// Version. A replacement never rewrites this version; it creates another one.
type AssetVersionState string

const (
	StateUploaded      AssetVersionState = "UPLOADED"
	StateQuarantined   AssetVersionState = "QUARANTINED"
	StateScanning      AssetVersionState = "SCANNING"
	StateScanPassed    AssetVersionState = "SCAN_PASSED"
	StateScanFailed    AssetVersionState = "SCAN_FAILED"
	StateScanError     AssetVersionState = "SCAN_ERROR"
	StateProcessing    AssetVersionState = "PROCESSING"
	StateReady         AssetVersionState = "READY"
	StateProcessFailed AssetVersionState = "PROCESS_FAILED"
)

// Valid reports whether s is one of the states owned by this state machine.
func (s AssetVersionState) Valid() bool {
	switch s {
	case StateUploaded, StateQuarantined, StateScanning, StateScanPassed,
		StateScanFailed, StateScanError, StateProcessing, StateReady, StateProcessFailed:
		return true
	default:
		return false
	}
}

// Deliverable is the single media deliverability rule. Every state other than
// READY is unavailable, including future failure or waiting states added to
// this machine.
func (s AssetVersionState) Deliverable() bool { return s == StateReady }

// transitionTable is exhaustive by state. Retry always returns through
// quarantine and scanning; no retry path can skip malware scanning.
var transitionTable = map[AssetVersionState]map[AssetVersionState]struct{}{
	StateUploaded: {
		StateQuarantined: {},
	},
	StateQuarantined: {
		StateScanning: {},
	},
	StateScanning: {
		StateScanPassed: {},
		StateScanFailed: {},
		StateScanError:  {},
	},
	StateScanFailed: {
		StateQuarantined: {},
	},
	StateScanError: {
		StateQuarantined: {},
	},
	StateScanPassed: {
		StateProcessing: {},
		// Non-video asset kinds become READY immediately after an exact-version
		// successful scan. The worker and database trigger enforce that this
		// edge is never used for VIDEO assets.
		StateReady: {},
	},
	StateProcessing: {
		StateReady:         {},
		StateProcessFailed: {},
	},
	StateProcessFailed: {
		StateQuarantined: {},
	},
	StateReady: {},
}

// Transition validates one state change. Invalid and unknown states are both
// rejected; callers must never infer a transition from an unrecognised value.
func Transition(from, to AssetVersionState) error {
	if !from.Valid() || !to.Valid() {
		return fmt.Errorf("invalid asset version transition %q -> %q", from, to)
	}
	if _, ok := transitionTable[from][to]; !ok {
		return fmt.Errorf("invalid asset version transition %q -> %q", from, to)
	}
	return nil
}

// ScanTransition applies an exact scan outcome to a version in SCANNING.
// Keeping this mapping beside Transition prevents a scanner result from
// becoming a readiness decision in a second, subtly different location.
func ScanTransition(outcome ScanOutcome) (AssetVersionState, error) {
	switch outcome {
	case ScanPassed:
		return StateScanPassed, nil
	case ScanFailed:
		return StateScanFailed, nil
	case ScanError:
		return StateScanError, nil
	default:
		return "", fmt.Errorf("invalid scan outcome %q", outcome)
	}
}
