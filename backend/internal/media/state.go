package media

import "fmt"

// AssetVersionState is the byte-processing state of one immutable Asset
// Version. A replacement never rewrites this version; it creates another one.
type AssetVersionState string

const (
	StateUploaded    AssetVersionState = "UPLOADED"
	StateQuarantined AssetVersionState = "QUARANTINED"
	StateScanning    AssetVersionState = "SCANNING"
	StateScanPassed  AssetVersionState = "SCAN_PASSED"
	StateScanFailed  AssetVersionState = "SCAN_FAILED"
	StateScanError   AssetVersionState = "SCAN_ERROR"
	StateProcessing  AssetVersionState = "PROCESSING"
	// StatePlayable means one canonical rendition has been verified and
	// persisted for this exact version: the video can be streamed, while the
	// rest of the expected ladder is still being produced. It is reached only
	// from PROCESSING, in the same transaction that inserts the rendition row,
	// and it leaves only for READY. It is deliberately not deliverable: generic
	// deliverability still means the whole ladder finished.
	StatePlayable      AssetVersionState = "PLAYABLE"
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
		StatePlayable, StateReady, StateProcessFailed:
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
//
// This table mirrors the authoritative schema-40 database trigger
// `media_asset_versions_enforce_immutability`, which is what actually refuses
// an illegitimate state change. The kind-conditioned edges the trigger owns —
// the THUMBNAIL and non-video shortcuts to READY — are expressed there and not
// here, because this table is kind-agnostic by construction. Where an edge is
// unconditional in the trigger, it must appear here too: a Go representation
// that silently disagrees with the database is worse than no representation,
// because callers reason about it.
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
		// PLAYABLE is entered progressively, once the first canonical rendition
		// is verified and persisted. READY remains the direct edge taken when
		// the whole ladder is verified without an intermediate publication of
		// partial evidence.
		StatePlayable:      {},
		StateReady:         {},
		StateProcessFailed: {},
	},
	// A PLAYABLE version only ever improves. It has already published verified
	// bytes, so it may not regress to a failure or retry state: a later
	// enhancement failure leaves it PLAYABLE with its surviving renditions,
	// and only completion of the expected ladder moves it on to READY.
	StatePlayable: {
		StateReady: {},
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
