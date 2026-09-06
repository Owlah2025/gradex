//go:build integration

package httpapi

import (
	"github.com/google/uuid"
	"strings"
	"testing"
)

func prepareAuditThumbnail(t *testing.T, f *privilegedAuditFixture) {
	t.Helper()
	assetID := uuid.NewString()
	f.thumbnailID = uuid.NewString()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO media_assets(id,kind,owner_account_id,course_id,preview_origin_revision_id)
 VALUES($1::uuid,'THUMBNAIL',$2::uuid,$3::uuid,$4::uuid)`, assetID, f.instructorID, f.courseID, f.revisionID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.pool.Exec(f.ctx, `INSERT INTO media_asset_versions(id,logical_asset_id,kind,state,storage_object_key,storage_object_version,content_type,size_bytes,sha256_hex)
 VALUES($1::uuid,$2::uuid,'THUMBNAIL','UPLOADED',$1::text,'fixture-v1','image/jpeg',100,$3)`, f.thumbnailID, assetID, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.pool.Exec(f.ctx, `UPDATE media_asset_versions SET state='QUARANTINED' WHERE id=$1::uuid`, f.thumbnailID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.pool.Exec(f.ctx, `INSERT INTO media_thumbnail_variants(asset_version_id,source_object_version,source_sha256,width,height,card_key,large_key)
 VALUES($1::uuid,'fixture-v1',$2,800,450,$1::text||'/card',$1::text||'/large')`, f.thumbnailID, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.pool.Exec(f.ctx, `UPDATE media_asset_versions SET state='READY' WHERE id=$1::uuid`, f.thumbnailID)
	if err != nil {
		t.Fatal(err)
	}
}
