package media

import (
	"context"
	"errors"
	"testing"
)

func TestAssetVersionStateMachineAcceptsOnlyApprovedTransitions(t *testing.T) {
	valid := []struct {
		from AssetVersionState
		to   AssetVersionState
	}{
		{StateUploaded, StateQuarantined},
		{StateQuarantined, StateScanning},
		{StateScanning, StateScanPassed},
		{StateScanning, StateScanFailed},
		{StateScanning, StateScanError},
		{StateScanPassed, StateProcessing},
		{StateProcessing, StateReady},
		{StateProcessing, StateProcessFailed},
		{StateScanFailed, StateQuarantined},
		{StateScanError, StateQuarantined},
		{StateProcessFailed, StateQuarantined},
	}
	for _, tc := range valid {
		if err := Transition(tc.from, tc.to); err != nil {
			t.Errorf("Transition(%q, %q) = %v", tc.from, tc.to, err)
		}
	}

	for _, from := range []AssetVersionState{
		StateUploaded, StateQuarantined, StateScanning, StateScanPassed,
		StateScanFailed, StateScanError, StateProcessing, StateReady, StateProcessFailed,
	} {
		for _, to := range []AssetVersionState{
			StateUploaded, StateQuarantined, StateScanning, StateScanPassed,
			StateScanFailed, StateScanError, StateProcessing, StateReady, StateProcessFailed,
		} {
			allowed := false
			for _, tc := range valid {
				if tc.from == from && tc.to == to {
					allowed = true
					break
				}
			}
			if err := Transition(from, to); (err == nil) != allowed {
				t.Errorf("Transition(%q, %q) allowed=%t, err=%v", from, to, allowed, err)
			}
		}
	}

	if err := Transition("UNKNOWN", StateReady); err == nil {
		t.Fatal("unknown source state was accepted")
	}
	if StateScanError.Deliverable() || StateProcessFailed.Deliverable() || StateProcessing.Deliverable() {
		t.Fatal("a non-READY state is deliverable")
	}
	if !StateReady.Deliverable() {
		t.Fatal("READY is not deliverable")
	}
}

type scannerFunc func(context.Context, ObjectVersion) (ScanObservation, error)

func (f scannerFunc) Scan(ctx context.Context, object ObjectVersion) (ScanObservation, error) {
	return f(ctx, object)
}

func TestScanObservationBindsToExactObjectVersion(t *testing.T) {
	object := ObjectVersion{AssetVersionID: "version-1", StorageObjectKey: "quarantine/version-1", StorageObjectVersion: "object-v1"}
	adapter, err := NewScannerAdapter(scannerFunc(func(_ context.Context, got ObjectVersion) (ScanObservation, error) {
		return ScanObservation{AssetVersionID: got.AssetVersionID, StorageObjectVersion: got.StorageObjectVersion, Outcome: ScanPassed, ScannerIdentity: "test-scanner"}, nil
	}))
	if err != nil {
		t.Fatalf("NewScannerAdapter: %v", err)
	}

	observation, err := adapter.Scan(context.Background(), object)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	next, err := ApplyScanObservation(StateScanning, object, observation)
	if err != nil || next != StateScanPassed {
		t.Fatalf("ApplyScanObservation = %q, %v; want SCAN_PASSED", next, err)
	}

	replacement := ObjectVersion{AssetVersionID: "version-2", StorageObjectKey: object.StorageObjectKey, StorageObjectVersion: "object-v2"}
	if _, err := ApplyScanObservation(StateScanning, replacement, observation); !errors.Is(err, ErrStaleScanEvidence) {
		t.Fatalf("old scan applied to replacement: %v", err)
	}
	if _, err := ApplyScanObservation(StateScanning, ObjectVersion{AssetVersionID: object.AssetVersionID, StorageObjectKey: object.StorageObjectKey, StorageObjectVersion: "object-v2"}, observation); !errors.Is(err, ErrStaleScanEvidence) {
		t.Fatalf("old object version scan applied to replacement bytes: %v", err)
	}
}

func TestScannerFailureModesNeverProduceDeliverableStateIndividually(t *testing.T) {
	object := ObjectVersion{AssetVersionID: "version-1", StorageObjectKey: "quarantine/version-1", StorageObjectVersion: "object-v1"}
	cases := []struct {
		name        string
		construct   func() (*ScannerAdapter, error)
		wantOutcome ScanOutcome
	}{
		{
			name: "malware",
			construct: func() (*ScannerAdapter, error) {
				return NewScannerAdapter(scannerFunc(func(_ context.Context, o ObjectVersion) (ScanObservation, error) {
					return ScanObservation{AssetVersionID: o.AssetVersionID, StorageObjectVersion: o.StorageObjectVersion, Outcome: ScanFailed, ScannerIdentity: "test-scanner", Reason: ErrMalwareDetected.Error()}, nil
				}))
			},
			wantOutcome: ScanFailed,
		},
		{
			name: "scanner error",
			construct: func() (*ScannerAdapter, error) {
				return NewScannerAdapter(scannerFunc(func(context.Context, ObjectVersion) (ScanObservation, error) {
					return ScanObservation{}, errors.New("provider error")
				}))
			},
			wantOutcome: ScanError,
		},
		{
			name: "scanner timeout",
			construct: func() (*ScannerAdapter, error) {
				return NewScannerAdapter(scannerFunc(func(ctx context.Context, _ ObjectVersion) (ScanObservation, error) {
					return ScanObservation{}, ctx.Err()
				}))
			},
			wantOutcome: ScanError,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapter, err := tc.construct()
			if err != nil {
				t.Fatalf("construct: %v", err)
			}
			ctx := context.Background()
			if tc.name == "scanner timeout" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			observation, scanErr := adapter.Scan(ctx, object)
			if observation.Outcome != tc.wantOutcome {
				t.Fatalf("outcome = %q, want %q", observation.Outcome, tc.wantOutcome)
			}
			next, applyErr := ApplyScanObservation(StateScanning, object, observation)
			if tc.wantOutcome == ScanFailed {
				if scanErr != nil || applyErr != nil || next != StateScanFailed {
					t.Fatalf("malware result = %q, scan=%v apply=%v", next, scanErr, applyErr)
				}
			} else if next.Deliverable() || applyErr != nil {
				if applyErr != nil {
					t.Fatalf("failure outcome could not be represented: %v", applyErr)
				}
				t.Fatalf("failure mode became deliverable: %q", next)
			}
		})
	}

	t.Run("absent scanner", func(t *testing.T) {
		adapter, err := NewUnavailableScanner("LG-014 scanner is not configured")
		if err != nil {
			t.Fatalf("NewUnavailableScanner: %v", err)
		}
		wrapped, err := NewScannerAdapter(adapter)
		if err != nil {
			t.Fatalf("NewScannerAdapter: %v", err)
		}
		observation, scanErr := wrapped.Scan(context.Background(), object)
		if observation.Outcome != ScanError || scanErr == nil {
			t.Fatalf("absent scanner outcome=%q err=%v", observation.Outcome, scanErr)
		}
		next, applyErr := ApplyScanObservation(StateScanning, object, observation)
		if applyErr != nil {
			t.Fatalf("absent scanner observation was not representable: %v", applyErr)
		}
		if next.Deliverable() {
			t.Fatal("absent scanner became deliverable")
		}
	})

	t.Run("misconfigured scanner", func(t *testing.T) {
		if _, err := NewScannerAdapter(nil); !errors.Is(err, ErrScannerRequired) {
			t.Fatalf("nil scanner error = %v, want ErrScannerRequired", err)
		}
		if _, err := NewUnavailableScanner(""); !errors.Is(err, ErrScannerMisconfigured) {
			t.Fatalf("empty unavailable reason error = %v, want ErrScannerMisconfigured", err)
		}
		adapter := &ScannerAdapter{}
		observation, scanErr := adapter.Scan(context.Background(), object)
		if observation.Outcome != ScanError || !errors.Is(scanErr, ErrScannerMisconfigured) {
			t.Fatalf("misconfigured scanner outcome=%q err=%v", observation.Outcome, scanErr)
		}
		next, applyErr := ApplyScanObservation(StateScanning, object, observation)
		if applyErr != nil {
			t.Fatalf("misconfigured observation was not representable: %v", applyErr)
		}
		if next.Deliverable() {
			t.Fatal("misconfigured scanner became deliverable")
		}
	})
}
