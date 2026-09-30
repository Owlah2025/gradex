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
	if DirectPurchaseAccessGrantSchemaVersion != AutoDeviceReplacementSchemaVersion+1 {
		t.Fatalf("DirectPurchaseAccessGrantSchemaVersion = %d, want 44", DirectPurchaseAccessGrantSchemaVersion)
	}
	if AutoEnhancementRecoverySchemaVersion != DirectPurchaseAccessGrantSchemaVersion+1 {
		t.Fatalf("AutoEnhancementRecoverySchemaVersion = %d, want 45", AutoEnhancementRecoverySchemaVersion)
	}
	if LessonPublicPreviewSchemaVersion != AutoEnhancementRecoverySchemaVersion+1 ||
		AdminOperationsSchemaVersion != LessonPublicPreviewSchemaVersion+1 ||
		AdminUser360SchemaVersion != AdminOperationsSchemaVersion+1 ||
		AdminUser360HardeningSchemaVersion != AdminUser360SchemaVersion+1 ||
		InstructorProfilesSchemaVersion != AdminUser360HardeningSchemaVersion+1 ||
		CourseCompletionsSchemaVersion != InstructorProfilesSchemaVersion+1 ||
		CourseAnnouncementsSchemaVersion != CourseCompletionsSchemaVersion+1 ||
		CatalogSearchAnalyticsSchemaVersion != CourseAnnouncementsSchemaVersion+1 ||
		MaxSchemaVersion != CatalogSearchAnalyticsSchemaVersion {
		t.Fatalf("CourseAnnouncementsSchemaVersion = %d, CatalogSearchAnalyticsSchemaVersion = %d, MaxSchemaVersion = %d, want analytics %d", CourseAnnouncementsSchemaVersion, CatalogSearchAnalyticsSchemaVersion, MaxSchemaVersion, CourseAnnouncementsSchemaVersion+1)
	}
	if APIRequiredSchemaVersion != CatalogSearchAnalyticsSchemaVersion {
		t.Fatalf("APIRequiredSchemaVersion = %d, want %d", APIRequiredSchemaVersion, CatalogSearchAnalyticsSchemaVersion)
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
