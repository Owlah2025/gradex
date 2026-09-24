package db

import (
	"testing"
)

func TestSchemaConstants(t *testing.T) {
	if MinSchemaVersion != 2 {
		t.Fatalf("MinSchemaVersion = %d, want 2", MinSchemaVersion)
	}
	if SubjectDemandSignalSchemaVersion != 38 {
		t.Fatalf("SubjectDemandSignalSchemaVersion = %d, want 38", SubjectDemandSignalSchemaVersion)
	}
	if MediaPlayableEnumSchemaVersion != 39 {
		t.Fatalf("MediaPlayableEnumSchemaVersion = %d, want 39", MediaPlayableEnumSchemaVersion)
	}
	if MediaPlayableFoundationSchemaVersion != 40 {
		t.Fatalf("MediaPlayableFoundationSchemaVersion = %d, want 40", MediaPlayableFoundationSchemaVersion)
	}
	if EnhancementRecoveryFoundationSchemaVersion != 41 {
		t.Fatalf("EnhancementRecoveryFoundationSchemaVersion = %d, want 41", EnhancementRecoveryFoundationSchemaVersion)
	}
	if ActiveProcessingKindSchemaVersion != EnhancementRecoveryFoundationSchemaVersion+1 {
		t.Fatalf("ActiveProcessingKindSchemaVersion = %d, want 42", ActiveProcessingKindSchemaVersion)
	}
	if AutoDeviceReplacementSchemaVersion != 43 {
		t.Fatalf("AutoDeviceReplacementSchemaVersion = %d, want 43", AutoDeviceReplacementSchemaVersion)
	}
	if DirectPurchaseAccessGrantSchemaVersion != AutoDeviceReplacementSchemaVersion+1 ||
		MaxSchemaVersion != DirectPurchaseAccessGrantSchemaVersion {
		t.Fatalf("MaxSchemaVersion = %d, want 44", MaxSchemaVersion)
	}
	if EnhancementRecoveryFoundationSchemaVersion != MediaPlayableFoundationSchemaVersion+1 {
		t.Fatalf("EnhancementRecoveryFoundationSchemaVersion = %d, want MediaPlayableFoundationSchemaVersion + 1 (%d)",
			EnhancementRecoveryFoundationSchemaVersion, MediaPlayableFoundationSchemaVersion+1)
	}
	if MediaPlayableEnumSchemaVersion != SubjectDemandSignalSchemaVersion+1 {
		t.Fatalf("MediaPlayableEnumSchemaVersion = %d, want SubjectDemandSignalSchemaVersion + 1 (%d)",
			MediaPlayableEnumSchemaVersion, SubjectDemandSignalSchemaVersion+1)
	}
}
