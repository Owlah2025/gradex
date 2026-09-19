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
	if MaxSchemaVersion != MediaPlayableEnumSchemaVersion {
		t.Fatalf("MaxSchemaVersion = %d, want %d", MaxSchemaVersion, MediaPlayableEnumSchemaVersion)
	}
	if MediaPlayableEnumSchemaVersion != SubjectDemandSignalSchemaVersion+1 {
		t.Fatalf("MediaPlayableEnumSchemaVersion = %d, want SubjectDemandSignalSchemaVersion + 1 (%d)",
			MediaPlayableEnumSchemaVersion, SubjectDemandSignalSchemaVersion+1)
	}
}
