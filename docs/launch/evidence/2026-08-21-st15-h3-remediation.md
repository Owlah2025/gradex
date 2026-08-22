# ST-15 H3 remediation evidence

**Recorded:** 2026-08-21

This is a focused post-approval remediation for ST-15, which remains the
existing `E2E_PROVEN` row, **Protected Resource / Lab download**. It neither
changes the tracker nor adds a row.

## ST15-1 — approval-time protected file dependency validation

Course submission and approval now validate each revision-scoped `RESOURCE`
and `LAB_MATERIAL` attachment through the same exact-version provenance join
used by protected delivery. The check is performed in the submission/approval
transaction and proves all of the following together:

- the candidate Course/revision/stable Lesson/`lesson_files` relationship;
- a `READY` Asset Version of the matching kind;
- a non-retired parent media asset in the same Course; and
- passed scan or D-088 validation evidence for the Asset Version's exact
  `storage_object_version`.

The query takes shared locks on the selected candidate graph rows and media
rows. Public Preview remains on its separate scanner-only validation path.

Real-Postgres regression proof:

```text
cd backend && go test -tags=integration ./internal/catalog -run \
  "TestST15ApprovalRefusesRetiredProtectedLessonFileDependencies|TestD5ApprovalRevalidatesEveryDependencyClass" -count=1

PASS (10.583s)
```

The focused test covers both Resource and Lab Material. A valid scanned
attachment is submitted, its parent asset is retired before approval, and
approval returns `ASSET_VERSION_UNAVAILABLE`; the candidate remains pending
and `live_revision_id` remains the previous live revision.

## ST15-2 — BR-103 buyer tag on the primary download response

The primary lesson-file authorization response now serializes the existing
opaque `buyer_tag` only when the delivery service issued one. That is the
existing Lab Material per-Entitlement marker. Resource responses omit the
field. The client type preserves it without rendering or decoding it.

Focused proof:

```text
cd backend && go test -tags=integration ./internal/media -run \
  "TestST15LessonFileDownloadProjectsEveryLiveAttachmentAndPreservesRevisionIsolation|TestD8DownloadsTagOnlyLabMaterialAndNeverExposeStudentPII" -count=1
PASS (2.604s)

cd backend && go test ./internal/httpapi -run "TestLessonFileDownloadAuthorization" -count=1
PASS (0.008s)
```

The real delivery test verifies a non-empty Lab tag has no Student identifier
or email, Resource has no tag, and anonymous/unentitled authorization is the
same unavailable result with no tag. The HTTP route test verifies the primary
JSON response serializes only an already-issued opaque tag and no identity or
storage identifiers.

## ST15-4 — Course Home material-contract test currency

ST-15 intentionally projects the minimum entitled-Student material metadata
needed to present and authorize protected downloads: localized `title`,
human-readable `file_type`, `size_bytes`, and an opaque
`download_authorization_path`. It keeps the D-011 / BR-067 domain distinction
as separate `resources` and `lab_materials` arrays, rather than serializing an
internal media-kind enum. This is the approved ST-15 design's explicit
read-model change and remains within D-063's protected Course Home
presentation boundary; it exposes neither storage identities nor signed
delivery URLs.

D-064's earlier `materials` kind-only wording is superseded for this ST-15
projection by the approved 2026-08-21 ST-15 design, which explicitly requires
localized filename, safe type label, size, and opaque same-origin
authorization route. There is therefore no unresolved authority conflict and
no production change in this test-currency remediation.

The `materials` to `resources` + `lab_materials` wire-contract change left two
pre-existing HTTP integration assertions stale:

- `TestD064LearningReadModelsExposeActiveKindsAndHideRetainedExpiryActions`;
- `TestT059SharedInstructorCourseHomesDoNotLeakGraphProgressOrMaterials`.

The production invariants remained correct. The tests now decode the response
and prove the separate collections semantically, including retained-expiry
hiding for D-064 and Course A/B graph, progress, material identity, and
authorization-path isolation for T059. The latter explicitly fails if a
foreign Course's material title, Lesson identity, or authorization path is
projected. D-064 also seeds a distinct Resource on a DRAFT revision and
requires the live Course Home and Lesson responses to contain only the live
Resource.

Focused proof:

```text
cd backend && go test -tags=integration ./internal/httpapi -run \
  '^TestD064LearningReadModelsExposeActiveKindsAndHideRetainedExpiryActions$' -count=1
PASS (1.254s)

cd backend && go test -tags=integration ./internal/httpapi -run \
  '^TestT059SharedInstructorCourseHomesDoNotLeakGraphProgressOrMaterials$' -count=1
PASS (1.048s)
```

The single-package gate was also rerun with `-p 1`. It emitted no D-064/T059
wire-shape failure and no fixture database create/drop conflict, but did not
complete within an explicit three-minute timeout because the unrelated
`TestProductionPrivilegedMutationRoutesCommitAuditEvidence` was blocked in
fresh-schema migration setup. That package timeout is not treated as ST-15
evidence or attributed to the corrected assertions.

## Regression gates

```text
cd backend && go build ./... && go vet ./... && go test ./...
PASS

cd frontend && npm run test -- --test-name-pattern="material download authorization" && npm run typecheck
PASS: frontend suite 297 passed, 0 failed; typecheck passed

cd frontend && npx playwright test --config=playwright.media-authoring.config.ts \
  s15-protected-materials.spec.ts --workers=1 --reporter=line
PASS: 1 passed
run mt30wtr293wmrvyv
API log: /var/tmp/gradex-s5-e2e-api-mt30wtr293wmrvyv.log
```

The browser run uses the separate real-storage media-authoring configuration:
real private MinIO fixture bytes are downloaded through the Student-facing
authorization route for both Resource and Lab Material, and the existing
revision-isolation journey remains green.

`go test -p 1 -tags=integration ./...` was also attempted. Its catalog and
media packages passed, including the focused regression above. The command
record is retained as historical output, but any earlier attribution of the
named D-064 and T059 HTTP failures to shared fixture databases or concurrent
create/drop conflicts was incorrect: they are reproducible stale assertions
against the former `materials` wire shape. They are corrected by the ST15-4
semantic response-contract updates above, not by an infrastructure change.
