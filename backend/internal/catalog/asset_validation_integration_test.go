//go:build integration

package catalog

import (
	"errors"
	"testing"
)

// The Instructor authoring UI attaches Asset Versions the S4 media pipeline
// produced, which live in `media_asset_versions`. Before this test the
// validator read only the legacy `videos` table, so a genuinely uploaded,
// scanned, and processed video was rejected as an invalid reference and no
// Instructor could ever attach one through the browser.
func TestAssetVersionValidationUsesTheMediaPipeline(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)

	accountID, courseID := seedInstructorAndCourse(t, p, ctx)
	validator := NewDBAssetVersionValidator(p)

	logicalAssetID := "33333333-3333-3333-3333-333333333333"
	if _, err := p.Exec(ctx, `
		INSERT INTO media_assets (id, kind, owner_account_id, course_id, visibility)
		VALUES ($1, 'VIDEO', $2, $3, 'PROTECTED')
	`, logicalAssetID, accountID, courseID); err != nil {
		t.Fatalf("seeding logical media asset: %v", err)
	}

	for _, testCase := range []struct {
		name           string
		assetVersionID string
		state          string
		wantErr        error
	}{
		{name: "ready", assetVersionID: "44444444-4444-4444-4444-444444444401", state: "READY"},
		{name: "quarantined", assetVersionID: "44444444-4444-4444-4444-444444444402", state: "QUARANTINED", wantErr: ErrAssetVersionNotReady},
		{name: "scan passed but not processed", assetVersionID: "44444444-4444-4444-4444-444444444403", state: "SCAN_PASSED", wantErr: ErrAssetVersionNotReady},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := p.Exec(ctx, `
				INSERT INTO media_asset_versions (
					id, logical_asset_id, kind, state, storage_object_key,
					storage_object_version, content_type, size_bytes
				) VALUES ($1, $2, 'VIDEO', $3::media_asset_version_state, $4, 'v1', 'video/mp4', 1024)
			`, testCase.assetVersionID, logicalAssetID, testCase.state, "quarantine/"+courseID+"/"+testCase.assetVersionID+"/source"); err != nil {
				t.Fatalf("seeding asset version: %v", err)
			}

			err := validator.ValidateAssetVersion(ctx, testCase.assetVersionID)
			if testCase.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateAssetVersion(%s) = %v, want nil", testCase.state, err)
				}
				return
			}
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("ValidateAssetVersion(%s) = %v, want %v", testCase.state, err, testCase.wantErr)
			}
		})
	}

	if err := validator.ValidateAssetVersion(ctx, "44444444-4444-4444-4444-4444444444ff"); !errors.Is(err, ErrAssetVersionInvalid) {
		t.Fatalf("unknown asset version = %v, want ErrAssetVersionInvalid", err)
	}
}
