//go:build integration

package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The combined candidate really does move production from schema 34 to schema
// 38, applying 0035, 0036, 0037 and 0038 in one release. These tests rehearse
// that exact chain against representative schema-34 data in a disposable
// database, and pin the readiness floor the D-106 routes require.
//
// They deliberately assert physical objects — tables, columns, constraints and
// indexes the new API and worker actually depend on — rather than only that the
// version counter reached 38. A migration that bumped the counter without
// creating what the code reads would still satisfy a version-only assertion and
// would still take the deployment down.

func columnExists(t *testing.T, pool *pgxpool.Pool, table, column string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	var exists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
		)`, table, column).Scan(&exists); err != nil {
		t.Fatalf("checking column %s.%s: %v", table, column, err)
	}
	return exists
}

func constraintExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	var exists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = $1)
	`, name).Scan(&exists); err != nil {
		t.Fatalf("checking constraint %s: %v", name, err)
	}
	return exists
}

func indexExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	var exists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname = 'public' AND indexname = $1)
	`, name).Scan(&exists); err != nil {
		t.Fatalf("checking index %s: %v", name, err)
	}
	return exists
}

func schemaVersion(t *testing.T, pool *pgxpool.Pool) (int64, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	state, err := ReadSchemaState(ctx, pool)
	if err != nil {
		t.Fatalf("reading schema state: %v", err)
	}
	return state.Version, state.Dirty
}

// representativeSchema34Data seeds the record classes a real production database
// holds at schema 34 and that the 34 -> 38 chain must not disturb: an Account, a
// Course with its media Asset Version in a terminal state, and an Entitlement.
// It returns a fingerprint the test compares after the chain has been applied.
type schema34Fingerprint struct {
	accountID      string
	instructorID   string
	courseID       string
	revisionID     string
	assetVersionID string
	assetState     string
	entitlements   int
}

func seedRepresentativeSchema34Data(t *testing.T, pool *pgxpool.Pool) schema34Fingerprint {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	var fingerprint schema34Fingerprint
	if err := pool.QueryRow(ctx, `
		INSERT INTO accounts (normalized_email, email, role, status, display_name, locale, email_verified_at)
		VALUES ('chain34-student@example.test', 'chain34-student@example.test', 'STUDENT', 'ACTIVE', 'Chain Student', 'en', now())
		RETURNING id::text
	`).Scan(&fingerprint.accountID); err != nil {
		t.Fatalf("seeding student account: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO accounts (normalized_email, email, role, status, display_name, locale, email_verified_at)
		VALUES ('chain34-instructor@example.test', 'chain34-instructor@example.test', 'INSTRUCTOR', 'ACTIVE', 'Chain Instructor', 'en', now())
		RETURNING id::text
	`).Scan(&fingerprint.instructorID); err != nil {
		t.Fatalf("seeding instructor account: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO courses (owner_account_id, lifecycle)
		VALUES ($1::uuid, 'DRAFT')
		RETURNING id::text
	`, fingerprint.instructorID).Scan(&fingerprint.courseID); err != nil {
		t.Fatalf("seeding course: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO course_revisions (course_id, state, revision_number, title_ar, title_en)
		VALUES ($1::uuid, 'DRAFT', 1, 'مادة السلسلة', 'Chain Course')
		RETURNING id::text
	`, fingerprint.courseID).Scan(&fingerprint.revisionID); err != nil {
		t.Fatalf("seeding course revision: %v", err)
	}

	var assetID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO media_assets (owner_account_id, course_id, kind)
		VALUES ($1::uuid, $2::uuid, 'VIDEO') RETURNING id::text
	`, fingerprint.instructorID, fingerprint.courseID).Scan(&assetID); err != nil {
		t.Fatalf("seeding media asset: %v", err)
	}
	// UPLOADED is a terminal-for-our-purposes resting state that needs no scan,
	// validation, or rendition provenance to be legitimate, so it seeds cleanly
	// without reaching into the media state machine.
	if err := pool.QueryRow(ctx, `
		INSERT INTO media_asset_versions (
			logical_asset_id, kind, state, storage_object_key, storage_object_version,
			content_type, size_bytes
		) VALUES ($1::uuid, 'VIDEO', 'UPLOADED', 'chain/34/object.mp4', 'v1', 'video/mp4', 2048)
		RETURNING id::text
	`, assetID).Scan(&fingerprint.assetVersionID); err != nil {
		t.Fatalf("seeding media asset version: %v", err)
	}
	fingerprint.assetState = "UPLOADED"

	// A real ACTIVE Entitlement, so the survival assertion below compares a
	// non-zero count. An earlier revision counted an empty table and proved
	// 0 == 0, which no migration could have failed.
	//
	// It needs the approved invitation it was granted from: ent_manual_needs_invitation
	// requires MANUAL_INVITATION entitlements to carry their provenance, which is
	// exactly the invariant the backup/restore checks also assert on.
	var invitationID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO course_access_invitations (
			normalized_email, email, course_id, created_by_account_id,
			decided_by_account_id, accepted_by_account_id, state, accepted_at, decided_at
		) VALUES ('chain34-student@example.test', 'chain34-student@example.test', $1::uuid, $2::uuid,
		          $2::uuid, $3::uuid, 'APPROVED', now(), now())
		RETURNING id::text
	`, fingerprint.courseID, fingerprint.instructorID, fingerprint.accountID).Scan(&invitationID); err != nil {
		t.Fatalf("seeding course access invitation: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO entitlements (
			student_account_id, scope_kind, scope_id, course_id, grant_source,
			source_invitation_id, original_access_ends_at, access_ends_at,
			retirement_eligibility_at, state
		) VALUES ($1::uuid, 'COURSE', $2::uuid, $2::uuid, 'MANUAL_INVITATION', $3::uuid,
		          now() + interval '365 days', now() + interval '365 days',
		          now() + interval '730 days', 'ACTIVE')
	`, fingerprint.accountID, fingerprint.courseID, invitationID); err != nil {
		t.Fatalf("seeding entitlement: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM entitlements
	`).Scan(&fingerprint.entitlements); err != nil {
		t.Fatalf("counting entitlements: %v", err)
	}
	if fingerprint.entitlements == 0 {
		t.Fatal("the entitlement seed produced no rows; the survival assertion would prove nothing")
	}
	return fingerprint
}

func assertSchema34DataSurvived(t *testing.T, pool *pgxpool.Pool, want schema34Fingerprint) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	var email, status string
	if err := pool.QueryRow(ctx, `
		SELECT email, status FROM accounts WHERE id = $1::uuid
	`, want.accountID).Scan(&email, &status); err != nil {
		t.Fatalf("seeded account did not survive the chain: %v", err)
	}
	if email != "chain34-student@example.test" || status != "ACTIVE" {
		t.Fatalf("account = %s/%s, want chain34-student@example.test/ACTIVE", email, status)
	}

	var lifecycle, ownerID string
	if err := pool.QueryRow(ctx, `
		SELECT lifecycle::text, owner_account_id::text FROM courses WHERE id = $1::uuid
	`, want.courseID).Scan(&lifecycle, &ownerID); err != nil {
		t.Fatalf("seeded course did not survive the chain: %v", err)
	}
	if lifecycle != "DRAFT" || ownerID != want.instructorID {
		t.Fatalf("course = %s owned by %s, want DRAFT owned by %s", lifecycle, ownerID, want.instructorID)
	}

	var titleEn, titleAr string
	if err := pool.QueryRow(ctx, `
		SELECT title_en, title_ar FROM course_revisions WHERE id = $1::uuid
	`, want.revisionID).Scan(&titleEn, &titleAr); err != nil {
		t.Fatalf("seeded course revision did not survive the chain: %v", err)
	}
	if titleEn != "Chain Course" || titleAr != "مادة السلسلة" {
		t.Fatalf("course revision titles = %q/%q, want the seeded pair", titleEn, titleAr)
	}

	var state, objectKey string
	if err := pool.QueryRow(ctx, `
		SELECT state::text, storage_object_key FROM media_asset_versions WHERE id = $1::uuid
	`, want.assetVersionID).Scan(&state, &objectKey); err != nil {
		t.Fatalf("seeded media asset version did not survive the chain: %v", err)
	}
	if state != want.assetState || objectKey != "chain/34/object.mp4" {
		t.Fatalf("media asset version = %s/%s, want %s/chain/34/object.mp4", state, objectKey, want.assetState)
	}

	var entitlements int
	var entitlementState, grantSource string
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM entitlements`).Scan(&entitlements); err != nil {
		t.Fatalf("counting entitlements after the chain: %v", err)
	}
	if entitlements != want.entitlements || entitlements == 0 {
		t.Fatalf("entitlement count = %d, want %d and non-zero", entitlements, want.entitlements)
	}
	// Not just the count: the access grant itself must be intact, since a
	// migration that preserved the row but cleared its state or provenance would
	// still have destroyed the Student's access.
	if err := pool.QueryRow(ctx, `
		SELECT state, grant_source FROM entitlements WHERE student_account_id = $1::uuid
	`, want.accountID).Scan(&entitlementState, &grantSource); err != nil {
		t.Fatalf("seeded entitlement did not survive the chain: %v", err)
	}
	if entitlementState != "ACTIVE" || grantSource != "MANUAL_INVITATION" {
		t.Fatalf("entitlement = %s/%s, want ACTIVE/MANUAL_INVITATION", entitlementState, grantSource)
	}
}

// postMigrationFixture is representative data for structures that DO NOT EXIST
// at schema 34 and therefore cannot be seeded before the chain runs. It is
// created after the migration that introduces each one, so the reverse path has
// real rows to destroy and the documented consequences can be verified instead
// of assumed. No impossible schema-34 row is invented.
type postMigrationFixture struct {
	accountID       string
	trustedDeviceID string
	deviceOTPID     string
	deviceEventID   string
	subjectID       string
}

// seedTrustedDeviceRows exercises 0037: a trusted device, its replacement
// cooldown state, a live device-trust OTP challenge, and a device security
// event. All four are classes the 0037 down migration deletes.
func seedTrustedDeviceRows(t *testing.T, pool *pgxpool.Pool, accountID string) postMigrationFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	fixture := postMigrationFixture{accountID: accountID}
	if err := pool.QueryRow(ctx, `
		INSERT INTO identity_trusted_devices (
			account_id, credential_digest, label, browser_family, platform_family, trusted_at
		) VALUES ($1::uuid, repeat('a', 64), 'Chain Laptop', 'Firefox', 'Linux', now())
		RETURNING id::text
	`, accountID).Scan(&fixture.trustedDeviceID); err != nil {
		t.Fatalf("seeding trusted device: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO identity_device_replacement_state (account_id, last_replacement_at)
		VALUES ($1::uuid, now())
	`, accountID); err != nil {
		t.Fatalf("seeding replacement cooldown state: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO identity_action_secrets (
			account_id, purpose, secret_digest, expires_at, trusted_device_id
		) VALUES ($1::uuid, 'DEVICE_TRUST_OTP', decode(repeat('ab', 32), 'hex'), now() + interval '10 minutes', $2::uuid)
		RETURNING id::text
	`, accountID, fixture.trustedDeviceID).Scan(&fixture.deviceOTPID); err != nil {
		t.Fatalf("seeding device trust OTP: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO identity_security_events (event_type, account_id, request_id)
		VALUES ('DEVICE_TRUSTED', $1::uuid, 'chain-rehearsal')
		RETURNING id::text
	`, accountID).Scan(&fixture.deviceEventID); err != nil {
		t.Fatalf("seeding device security event: %v", err)
	}
	return fixture
}

// seedTrustedDeviceRowsWithoutHistory seeds only the two classes whose presence
// does NOT block the 0037 down migration: the trusted-device registration and
// the replacement cooldown state, both of which are removed by DROP TABLE. It
// models a deployment that migrated device trust in but never exercised it, and
// that is the only shape in which the reverse chain can complete at all.
func seedTrustedDeviceRowsWithoutHistory(t *testing.T, pool *pgxpool.Pool, accountID string) postMigrationFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	fixture := postMigrationFixture{accountID: accountID}
	if err := pool.QueryRow(ctx, `
		INSERT INTO identity_trusted_devices (
			account_id, credential_digest, label, browser_family, platform_family, trusted_at
		) VALUES ($1::uuid, repeat('a', 64), 'Chain Laptop', 'Firefox', 'Linux', now())
		RETURNING id::text
	`, accountID).Scan(&fixture.trustedDeviceID); err != nil {
		t.Fatalf("seeding trusted device: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO identity_device_replacement_state (account_id, last_replacement_at)
		VALUES ($1::uuid, now())
	`, accountID); err != nil {
		t.Fatalf("seeding replacement cooldown state: %v", err)
	}
	return fixture
}

// seedSubjectDemandRow exercises 0038 with a real Student demand signal, which
// requires the Institution and Subject it is pinned to.
func seedSubjectDemandRow(t *testing.T, pool *pgxpool.Pool, accountID string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	var institutionID, subjectID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO institutions (country_code, slug, name_ar, name_en)
		VALUES ('KW', 'chain-rehearsal-university', 'جامعة السلسلة', 'Chain Rehearsal University')
		RETURNING id::text
	`).Scan(&institutionID); err != nil {
		t.Fatalf("seeding institution: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO subjects (institution_id, official_code, title_ar, title_en)
		VALUES ($1::uuid, 'CHN 101', 'مادة السلسلة', 'Chain Subject')
		RETURNING id::text
	`, institutionID).Scan(&subjectID); err != nil {
		t.Fatalf("seeding subject: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO subject_demand_signals (account_id, subject_id, institution_id, note)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'I take this in the fall')
	`, accountID, subjectID, institutionID); err != nil {
		t.Fatalf("seeding subject demand signal: %v", err)
	}
	return subjectID
}

func rowCount(t *testing.T, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	var count int
	if err := pool.QueryRow(ctx, query, args...).Scan(&count); err != nil {
		t.Fatalf("counting rows: %v", err)
	}
	return count
}

// TestSchema34To38ChainAppliesCleanlyOverRealData is the release rehearsal: a
// clean schema-34 database holding representative data is carried to a clean
// schema 38, one migration at a time, and every physical object the new API and
// worker read is asserted to exist at the end.
func TestSchema34To38ChainAppliesCleanlyOverRealData(t *testing.T) {
	freshDatabase(t)
	pool := openPool(t)
	m := openMigrator(t)

	// 1. A clean schema 34, which is where production actually is.
	if err := m.Migrate(uint(CourseThumbnailSchemaVersion + 1)); err != nil {
		t.Fatalf("migrating to schema 34: %v", err)
	}
	if version, dirty := schemaVersion(t, pool); version != 34 || dirty {
		t.Fatalf("starting schema = %d dirty=%t, want 34 clean", version, dirty)
	}
	fingerprint := seedRepresentativeSchema34Data(t, pool)

	// 34 must genuinely lack everything the later migrations introduce, or the
	// rest of this test would be asserting nothing.
	for _, absent := range []struct{ table, column string }{
		{"media_asset_versions", "work_claim_token"},
		{"media_asset_versions", "processing_attempt_count"},
	} {
		if columnExists(t, pool, absent.table, absent.column) {
			t.Fatalf("%s.%s exists at schema 34", absent.table, absent.column)
		}
	}
	for _, absent := range []string{"bundles", "identity_trusted_devices", "subject_demand_signals"} {
		if tableExists(t, pool, absent) {
			t.Fatalf("table %s exists at schema 34", absent)
		}
	}

	// 2. One migration at a time, each landing clean.
	for _, step := range []struct {
		version int64
		name    string
	}{
		{MediaWorkLeaseSchemaVersion, "0035_media_work_leases"},
		{BundlesAndOffersSchemaVersion, "0036_bundles_and_offers"},
		{StudentTrustedDeviceSchemaVersion, "0037_student_trusted_devices"},
		{SubjectDemandSignalSchemaVersion, "0038_subject_demand_signals"},
	} {
		if err := m.Migrate(uint(step.version)); err != nil {
			t.Fatalf("applying %s: %v", step.name, err)
		}
		version, dirty := schemaVersion(t, pool)
		if version != step.version || dirty {
			t.Fatalf("after %s schema = %d dirty=%t, want %d clean", step.name, version, dirty, step.version)
		}
	}

	// 3. A clean 38.
	if version, dirty := schemaVersion(t, pool); version != SubjectDemandSignalSchemaVersion || dirty {
		t.Fatalf("final schema = %d dirty=%t, want %d clean", version, dirty, SubjectDemandSignalSchemaVersion)
	}

	// 4. The pre-existing data survived unchanged.
	assertSchema34DataSurvived(t, pool, fingerprint)

	// 5. The physical objects the D-103 worker reads.
	for _, column := range []string{
		"work_claim_token", "work_claimed_at", "work_lease_expires_at",
		"scan_attempt_count", "processing_attempt_count", "last_failure_category",
	} {
		if !columnExists(t, pool, "media_asset_versions", column) {
			t.Errorf("media_asset_versions.%s is missing after the chain", column)
		}
	}
	for _, constraint := range []string{
		"media_asset_versions_work_claim_coherent",
		"media_asset_versions_scan_attempt_count_non_negative",
		"media_asset_versions_processing_attempt_count_non_negative",
		"media_asset_versions_failure_category_known",
	} {
		if !constraintExists(t, pool, constraint) {
			t.Errorf("constraint %s is missing after the chain", constraint)
		}
	}
	if !indexExists(t, pool, "media_asset_versions_expired_work_lease_idx") {
		t.Error("the expired-work-lease recovery index is missing after the chain")
	}

	// 6. The physical objects the D-106 API reads.
	for _, table := range []string{"identity_trusted_devices", "identity_device_replacement_state", "subject_demand_signals"} {
		if !tableExists(t, pool, table) {
			t.Errorf("table %s is missing after the chain", table)
		}
	}
	for _, column := range []string{"account_id", "subject_id", "institution_id", "note", "withdrawn_at"} {
		if !columnExists(t, pool, "subject_demand_signals", column) {
			t.Errorf("subject_demand_signals.%s is missing after the chain", column)
		}
	}
	for _, index := range []string{
		"subject_demand_signals_live_unique",
		"subject_demand_signals_institution_subject_idx",
		"subject_demand_signals_account_idx",
	} {
		if !indexExists(t, pool, index) {
			t.Errorf("index %s is missing after the chain", index)
		}
	}
	for _, constraint := range []string{
		"subject_demand_signals_subject_same_institution",
		"subject_demand_signals_note_length",
		"subject_demand_signals_withdrawn_after_created",
	} {
		if !constraintExists(t, pool, constraint) {
			t.Errorf("constraint %s is missing after the chain", constraint)
		}
	}

	// 7. The bundle tables 0036 is responsible for.
	for _, table := range []string{"bundles", "bundle_courses", "bundle_price_changes"} {
		if !tableExists(t, pool, table) {
			t.Errorf("table %s is missing after the chain", table)
		}
	}
}

// TestSchema38ReadinessFloorRefusesSchema37 is the H3 proof. The floor is the
// API's own requiredSchemaVersion value, expressed here as the constant that
// function returns.
func TestSchema38ReadinessFloorRefusesSchema37(t *testing.T) {
	freshDatabase(t)
	pool := openPool(t)
	m := openMigrator(t)

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	// Schema 37: trusted devices present, subject_demand_signals absent.
	if err := m.Migrate(uint(StudentTrustedDeviceSchemaVersion)); err != nil {
		t.Fatalf("migrating to schema 37: %v", err)
	}
	if tableExists(t, pool, "subject_demand_signals") {
		t.Fatal("subject_demand_signals exists at schema 37")
	}
	err := CheckSchemaAtLeast(ctx, pool, SubjectDemandSignalSchemaVersion)
	if !errors.Is(err, ErrSchemaIncompatible) {
		t.Fatalf("schema 37 accepted by the D-106 floor: %v", err)
	}
	if msg := err.Error(); !strings.Contains(msg, fmt.Sprint(StudentTrustedDeviceSchemaVersion)) {
		t.Errorf("the refusal should name the version found, got %q", msg)
	}

	// Schema 38: accepted.
	if err := m.Migrate(uint(SubjectDemandSignalSchemaVersion)); err != nil {
		t.Fatalf("migrating to schema 38: %v", err)
	}
	if !tableExists(t, pool, "subject_demand_signals") {
		t.Fatal("subject_demand_signals is missing at schema 38")
	}
	if err := CheckSchemaAtLeast(ctx, pool, SubjectDemandSignalSchemaVersion); err != nil {
		t.Fatalf("schema 38 refused by the D-106 floor: %v", err)
	}

	// Above the ceiling: still refused by the existing range behaviour, so
	// raising the floor did not disturb the upper bound.
	if _, err := pool.Exec(ctx,
		"UPDATE "+schemaMigrationsTable+" SET version = $1", MaxSchemaVersion+1); err != nil {
		t.Fatalf("setting an above-ceiling version: %v", err)
	}
	if err := CheckSchemaAtLeast(ctx, pool, SubjectDemandSignalSchemaVersion); !errors.Is(err, ErrSchemaIncompatible) {
		t.Fatalf("a schema above the build ceiling was accepted: %v", err)
	}

	// A dirty marker is refused regardless of version, which is what keeps a
	// half-applied chain out of the load balancer.
	if _, err := pool.Exec(ctx,
		"UPDATE "+schemaMigrationsTable+" SET version = $1, dirty = true", SubjectDemandSignalSchemaVersion); err != nil {
		t.Fatalf("setting a dirty marker: %v", err)
	}
	if err := CheckSchemaAtLeast(ctx, pool, SubjectDemandSignalSchemaVersion); !errors.Is(err, ErrSchemaDirty) {
		t.Fatalf("a dirty schema 38 was accepted: %v", err)
	}
}

// TestSchema37RollbackIsRefusedByRealDeviceData records two independent refusal
// conditions, both found by putting a representative row in front of the
// migration instead of reversing an empty database.
//
//  1. identity_security_events carries an append-only BEFORE UPDATE OR DELETE
//     trigger from 0005. 0037's down migration must DELETE the nine device event
//     types, because the pre-0037 CHECK constraint it restores would otherwise be
//     violated by history the feature produced. Those two requirements are
//     irreconcilable: one device security event is enough to stop the rollback.
//
//  2. 0037's down migration deletes live DEVICE_TRUST_OTP rows and then ALTERs
//     identity_action_secrets in the same transaction. PostgreSQL refuses to
//     ALTER a table that has pending trigger events from earlier DML in that
//     transaction, so one live device-trust challenge is also enough.
//
// Either condition arises the first time a Student uses device trust. The
// practical consequence for a release is that schema 37 is a FLOOR in any
// deployment where the feature has been exercised, exactly as schema 36 is a
// floor once Bundle commerce data exists. Reversing past it is a
// restore-from-backup operation, not a migration.
func TestSchema37RollbackIsRefusedByRealDeviceData(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		seed    func(t *testing.T, pool *pgxpool.Pool, accountID, deviceID string)
		wantErr string
	}{
		{
			name: "a device security event",
			seed: func(t *testing.T, pool *pgxpool.Pool, accountID, _ string) {
				ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
				defer cancel()
				if _, err := pool.Exec(ctx, `
					INSERT INTO identity_security_events (event_type, account_id, request_id)
					VALUES ('DEVICE_TRUSTED', $1::uuid, 'chain-rehearsal')
				`, accountID); err != nil {
					t.Fatalf("seeding device security event: %v", err)
				}
			},
			wantErr: "append-only",
		},
		{
			name: "a live device-trust OTP challenge",
			seed: func(t *testing.T, pool *pgxpool.Pool, accountID, deviceID string) {
				ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
				defer cancel()
				if _, err := pool.Exec(ctx, `
					INSERT INTO identity_action_secrets (
						account_id, purpose, secret_digest, expires_at, trusted_device_id
					) VALUES ($1::uuid, 'DEVICE_TRUST_OTP', decode(repeat('cd', 32), 'hex'),
					          now() + interval '10 minutes', $2::uuid)
				`, accountID, deviceID); err != nil {
					t.Fatalf("seeding device trust OTP: %v", err)
				}
			},
			wantErr: "pending trigger events",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			freshDatabase(t)
			pool := openPool(t)
			m := openMigrator(t)

			if err := m.Migrate(uint(SubjectDemandSignalSchemaVersion)); err != nil {
				t.Fatalf("up to schema 38: %v", err)
			}
			fingerprint := seedRepresentativeSchema34Data(t, pool)
			devices := seedTrustedDeviceRowsWithoutHistory(t, pool, fingerprint.accountID)
			testCase.seed(t, pool, fingerprint.accountID, devices.trustedDeviceID)

			// 38 -> 37 is unaffected and still succeeds.
			if err := m.Migrate(uint(StudentTrustedDeviceSchemaVersion)); err != nil {
				t.Fatalf("reversing 0038: %v", err)
			}

			err := m.Migrate(uint(BundlesAndOffersSchemaVersion))
			if err == nil {
				t.Fatal("reversing 0037 succeeded against real device data")
			}
			if !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("reversing 0037 failed with %v, want a refusal mentioning %q", err, testCase.wantErr)
			}

			// A refused rollback destroyed nothing.
			if !tableExists(t, pool, "identity_trusted_devices") {
				t.Fatal("identity_trusted_devices was dropped by a refused rollback")
			}
			if got := rowCount(t, pool, `SELECT count(*) FROM identity_trusted_devices`); got != 1 {
				t.Fatalf("trusted device rows = %d, want the row preserved by the refusal", got)
			}
		})
	}
}

// TestSchema38ReverseChainIsSupervisedAndRefusesDestructiveSteps rehearses the
// documented reverse path in disposable infrastructure, one migration at a time
// and in reverse order, and pins the point at which a down migration
// intentionally refuses rather than destroying live commercial data.
func TestSchema38ReverseChainIsSupervisedAndRefusesDestructiveSteps(t *testing.T) {
	freshDatabase(t)
	pool := openPool(t)
	m := openMigrator(t)

	if err := m.Migrate(uint(SubjectDemandSignalSchemaVersion)); err != nil {
		t.Fatalf("migrating to schema 38: %v", err)
	}
	if version, dirty := schemaVersion(t, pool); version != SubjectDemandSignalSchemaVersion || dirty {
		t.Fatalf("schema = %d dirty=%t, want %d clean", version, dirty, SubjectDemandSignalSchemaVersion)
	}

	// Representative rows for the structures that do not exist at schema 34, so
	// the reverse path destroys real data and the documented consequences are
	// verified rather than assumed.
	//
	// Device SECURITY EVENTS are deliberately not seeded here: they make the
	// 0037 down migration impossible outright, which is its own test
	// (TestSchema37RollbackIsRefusedOnceDeviceSecurityEventsExist). This case
	// covers the only shape in which the reverse chain can complete — a
	// deployment where device trust was migrated in but never exercised.
	fingerprint := seedRepresentativeSchema34Data(t, pool)
	seedTrustedDeviceRowsWithoutHistory(t, pool, fingerprint.accountID)
	seedSubjectDemandRow(t, pool, fingerprint.accountID)

	sessionsBefore := rowCount(t, pool, `SELECT count(*) FROM sessions`)
	if got := rowCount(t, pool, `SELECT count(*) FROM subject_demand_signals`); got != 1 {
		t.Fatalf("seeded demand signals = %d, want 1", got)
	}
	if got := rowCount(t, pool, `SELECT count(*) FROM identity_trusted_devices`); got != 1 {
		t.Fatalf("seeded trusted devices = %d, want 1", got)
	}

	// 38 -> 37 discards Student demand. That is real product input, and the
	// rehearsal proves the loss rather than glossing it.
	if err := m.Migrate(uint(StudentTrustedDeviceSchemaVersion)); err != nil {
		t.Fatalf("reversing 0038: %v", err)
	}
	if tableExists(t, pool, "subject_demand_signals") {
		t.Fatal("subject_demand_signals survived its own down migration")
	}
	if version, dirty := schemaVersion(t, pool); version != StudentTrustedDeviceSchemaVersion || dirty {
		t.Fatalf("after reversing 0038 schema = %d dirty=%t, want %d clean", version, dirty, StudentTrustedDeviceSchemaVersion)
	}
	// The Subject the demand pointed at is not collateral damage: 0038 owns only
	// the signal table, never the catalogue it references.
	if got := rowCount(t, pool, `SELECT count(*) FROM subjects`); got != 1 {
		t.Fatalf("subjects after reversing 0038 = %d, want the catalogue row preserved", got)
	}

	// 37 -> 36 deletes four separate classes of row, exactly as the release plan
	// documents: device security history, live device-trust OTP challenges,
	// trusted-device registrations, and replacement cooldown state.
	if err := m.Migrate(uint(BundlesAndOffersSchemaVersion)); err != nil {
		t.Fatalf("reversing 0037: %v", err)
	}
	for _, gone := range []string{"identity_trusted_devices", "identity_device_replacement_state"} {
		if tableExists(t, pool, gone) {
			t.Fatalf("%s survived its own down migration", gone)
		}
	}
	for _, column := range []string{"trusted_device_id", "device_trust_state"} {
		if columnExists(t, pool, "sessions", column) {
			t.Fatalf("sessions.%s survived the 0037 down migration", column)
		}
	}
	// And the documented non-consequence: no session is deleted, so no Student
	// is logged out by the rollback.
	if got := rowCount(t, pool, `SELECT count(*) FROM sessions`); got != sessionsBefore {
		t.Fatalf("sessions = %d, want %d — the rollback deleted sessions", got, sessionsBefore)
	}

	// 36 -> 35 removes the Bundle tables. On an empty commerce set this is
	// permitted; the guard that matters once real Bundle data exists is
	// exercised separately below.
	if err := m.Migrate(uint(MediaWorkLeaseSchemaVersion)); err != nil {
		t.Fatalf("reversing 0036: %v", err)
	}
	for _, gone := range []string{"bundles", "bundle_courses", "bundle_price_changes"} {
		if tableExists(t, pool, gone) {
			t.Fatalf("%s survived its own down migration", gone)
		}
	}

	// 35 -> 34 removes the media work-lease metadata only.
	if err := m.Migrate(uint(CourseThumbnailSchemaVersion + 1)); err != nil {
		t.Fatalf("reversing 0035: %v", err)
	}
	for _, gone := range []string{
		"work_claim_token", "work_claimed_at", "work_lease_expires_at",
		"scan_attempt_count", "processing_attempt_count", "last_failure_category",
	} {
		if columnExists(t, pool, "media_asset_versions", gone) {
			t.Fatalf("media_asset_versions.%s survived the 0035 down migration", gone)
		}
	}
	if version, dirty := schemaVersion(t, pool); version != 34 || dirty {
		t.Fatalf("after the full reverse chain schema = %d dirty=%t, want 34 clean", version, dirty)
	}
	// Everything that legitimately predates the chain is still intact: the
	// reverse path destroys only what its own migrations introduced.
	assertSchema34DataSurvived(t, pool, fingerprint)

	// And forward again, so the reverse path leaves a database that can still be
	// brought back up rather than a dead end.
	if err := m.Migrate(uint(SubjectDemandSignalSchemaVersion)); err != nil {
		t.Fatalf("re-applying the chain after the reverse path: %v", err)
	}
	if version, dirty := schemaVersion(t, pool); version != SubjectDemandSignalSchemaVersion || dirty {
		t.Fatalf("after re-applying schema = %d dirty=%t, want %d clean", version, dirty, SubjectDemandSignalSchemaVersion)
	}
}
