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

	// StateValidated records that the exact stored object version passed the
	// D-088 trusted-Instructor validation — configured size bound, actual
	// stored size, declared type against the real file format, and SHA-256
	// over that exact version. It deliberately does not claim, imply, or
	// substitute for malware scanning, and it is never produced by a scan
	// outcome.
	StateValidated AssetVersionState = "VALIDATED"
)

// Valid reports whether s is one of the states owned by this state machine.
func (s AssetVersionState) Valid() bool {
	switch s {
	case StateUploaded, StateQuarantined, StateScanning, StateScanPassed,
		StateScanFailed, StateScanError, StateValidated, StateProcessing,
		StateReady, StateProcessFailed:
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
// quarantine, and quarantine is the only entry to either safety path: a
// scanner-gated asset leaves it through SCANNING, and a D-088 trusted asset
// leaves it through VALIDATED. No retry path can skip the safety evidence its
// asset requires, and neither path can enter the other's states.
var transitionTable = map[AssetVersionState]map[AssetVersionState]struct{}{
	StateUploaded: {
		StateQuarantined: {},
	},
	StateQuarantined: {
		StateScanning: {},
		// D-088: only after exact-version validation evidence exists for this
		// object version. The service and the database trigger both enforce
		// that evidence; the table alone only says the edge is reachable.
		StateValidated: {},
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
	StateValidated: {
		// A validated video still owes the trusted FFmpeg evidence; only a
		// validated non-video D-088 Lesson Resource may become READY here.
		StateProcessing:    {},
		StateReady:         {},
		StateProcessFailed: {},
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
