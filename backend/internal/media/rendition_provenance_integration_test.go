//go:build integration

package media

import (
	"errors"
	"testing"
)

// TestRenditionProvenanceIsRecordedOnInsert proves the schema-41 write-only
// plumbing: every canonical rendition committed through the real progressive
// persistence path records the operation that committed it. Nothing reads the
// column to make a decision at this version — this test is what makes the write
// itself observable.
func TestRenditionProvenanceIsRecordedOnInsert(t *testing.T) {
	f, worker, _, versionID, opID := processingVideoReadyForRenditions(t)

	first := renditionFixture(versionID, opID, "720p", 1280, 720, 2800, 10000)
	if err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, first); err != nil {
		t.Fatalf("PersistVerifiedRendition: %v", err)
	}

	var provenance *string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT processing_operation_id FROM video_renditions
		WHERE asset_version_id = $1::uuid AND name = '720p'
	`, versionID).Scan(&provenance); err != nil {
		t.Fatalf("reading rendition provenance: %v", err)
	}
	if provenance == nil {
		t.Fatal("rendition provenance is NULL; a row written on schema 41 must record its operation")
	}
	if *provenance != opID {
		t.Fatalf("rendition provenance = %q, want the committing operation %q", *provenance, opID)
	}

	// A second rung of the same attempt records the same operation, and the
	// other persisted metadata is unaffected by the added column.
	second := renditionFixture(versionID, opID, "480p", 854, 480, 1400, 10000)
	if err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, second); err != nil {
		t.Fatalf("PersistVerifiedRendition for the second rung: %v", err)
	}
	var storedKey string
	var width, height, bitrate int
	var duration int64
	if err := f.pool.QueryRow(f.ctx, `
		SELECT processing_operation_id, storage_object_key, width, height, bitrate_kbps, duration_ms
		FROM video_renditions WHERE asset_version_id = $1::uuid AND name = '480p'
	`, versionID).Scan(&provenance, &storedKey, &width, &height, &bitrate, &duration); err != nil {
		t.Fatalf("reading the second rung: %v", err)
	}
	if provenance == nil || *provenance != opID {
		t.Fatalf("second rung provenance = %v, want %q", provenance, opID)
	}
	if storedKey != second.StorageObjectKey || width != 854 || height != 480 || bitrate != 1400 || duration != 10000 {
		t.Fatalf("second rung metadata was corrupted: key=%q %dx%d %dkbps %dms", storedKey, width, height, bitrate, duration)
	}
}

// TestRenditionProvenanceIdempotentReplay proves an ambiguous commit replayed by
// the same operation still succeeds, and that the recorded provenance is part of
// what the replay is checked against.
func TestRenditionProvenanceIdempotentReplay(t *testing.T) {
	f, worker, _, versionID, opID := processingVideoReadyForRenditions(t)
	rendition := renditionFixture(versionID, opID, "720p", 1280, 720, 2800, 10000)

	if err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, rendition); err != nil {
		t.Fatalf("first PersistVerifiedRendition: %v", err)
	}
	// Same operation, same key, same metadata, same recorded provenance.
	if err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, rendition); err != nil {
		t.Fatalf("idempotent replay of the same operation was refused: %v", err)
	}

	var rows int
	var provenance *string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT count(*), max(processing_operation_id) FROM video_renditions
		WHERE asset_version_id = $1::uuid AND name = '720p'
	`, versionID).Scan(&rows, &provenance); err != nil {
		t.Fatalf("counting rendition rows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("replay produced %d rows, want exactly 1", rows)
	}
	if provenance == nil || *provenance != opID {
		t.Fatalf("provenance after replay = %v, want %q", provenance, opID)
	}
}

// TestRenditionProvenanceCrossOperationStillConflicts pins the unchanged 3C-A
// behaviour: a different attempt claiming a rendition name that already exists
// is a conflict. The benign prior-attempt skip that multi-attempt recovery will
// need belongs to a later phase and must not appear here by accident.
func TestRenditionProvenanceCrossOperationStillConflicts(t *testing.T) {
	f, worker, _, versionID, opID := processingVideoReadyForRenditions(t)
	rendition := renditionFixture(versionID, opID, "720p", 1280, 720, 2800, 10000)
	if err := worker.PersistVerifiedRendition(f.ctx, versionID, opID, rendition); err != nil {
		t.Fatalf("PersistVerifiedRendition: %v", err)
	}

	// A foreign operation writes its own attempt-scoped key for the same rung.
	foreignOp := "foreign-operation"
	foreign := renditionFixture(versionID, foreignOp, "720p", 1280, 720, 2800, 10000)
	err := worker.PersistVerifiedRendition(f.ctx, versionID, foreignOp, foreign)
	if err == nil {
		t.Fatal("expected a foreign operation persisting an existing rendition name to be refused in 3C-A")
	}
	// It is refused at the claim fence before provenance is even consulted,
	// which is the pre-existing behaviour; the assertion is that it is refused.
	if !errors.Is(err, ErrConcurrentModification) && !errors.Is(err, ErrConflict) {
		t.Fatalf("foreign-operation persistence failed with %v, want a claim or conflict refusal", err)
	}

	var provenance *string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT processing_operation_id FROM video_renditions
		WHERE asset_version_id = $1::uuid AND name = '720p'
	`, versionID).Scan(&provenance); err != nil {
		t.Fatalf("reading rendition provenance: %v", err)
	}
	if provenance == nil || *provenance != opID {
		t.Fatalf("provenance = %v, want the original committing operation %q", provenance, opID)
	}
}
