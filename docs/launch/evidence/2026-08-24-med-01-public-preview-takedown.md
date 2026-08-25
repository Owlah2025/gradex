# MED-01 — Public Preview Emergency Takedown Consistency

Date: 2026-08-24
Repository: gradex-ui-antigravity
Branch: ui-antigravity-20260817
Authorization: Founder-authorized MED-01-only remediation tranche.

## 1. Overall verdict

**PARTIAL — MED-01 behavior is proven by focused real-PostgreSQL evidence; the repository-wide
canonical E2E gate cannot run green in the protected dirty worktree because migration 0027 is
present while the current API readiness build supports schema versions 14 through 26 and the
run-owned worker also fails its schema check.**

The MED-01-specific remediation proof is complete and the finding can be closed. The partial label
records the unrelated release-gate environment mismatch; it is not a MED-01 functional failure.

## 2. Remediation definition

MED-01 — Public Preview Emergency Takedown Consistency.

Scope was limited to anonymous/public preview issuance and its parity with public Course
eligibility. No protected Student playback, Entitlement authority, Course lifecycle design,
rate-limiting, security-header, Redis, backup, monitoring, frontend, deployment, or GAP-06 work
was performed.

## 3. Confirmed root cause

Before the change, both public media entry points called the shared media function:

~~~text
IssuePreview(ctx, assetVersionID)
  -> issuePreview(ctx, cr.preview_asset_version_id = $1::uuid, assetVersionID)

IssueCoursePreview(ctx, courseID)
  -> issuePreview(ctx, c.id = $1::uuid, courseID)
~~~

The shared SQL previously used this public Course condition:

~~~sql
AND c.lifecycle = 'PUBLISHED'
~~~

It then checked the current live revision, approved revision state, preview media kind and content
type, PUBLIC_PREVIEW visibility, scanner evidence, preview-origin lineage, READY state, and media
retirement. It did not check courses.access_suspended_at or courses.retired_at.

The public catalogue already used the canonical predicate below, so a PUBLISHED Course could be
hidden from public discovery while still satisfying the preview issuance query.

## 4. Canonical public eligibility

The existing catalogpublic.PublishedOnly("c", "cr") helper is:

~~~sql
c.lifecycle = 'PUBLISHED'
AND c.access_suspended_at IS NULL
AND c.retired_at IS NULL
AND c.live_revision_id = cr.id
~~~

The preview query now reuses that helper inside its existing single SQL lookup, and retains all
preview-specific checks after it. The current Course row is evaluated at authorization time.

## 5. Code change

Changed production code:

- backend/internal/media/delivery.go
  - imported backend/internal/catalogpublic;
  - replaced the duplicated lifecycle-only condition in issuePreview with
    catalogpublic.PublishedOnly("c", "cr").

Changed proof code:

- backend/internal/media/delivery_integration_test.go
  - added TestMED01PublicPreviewFollowsCanonicalCourseEligibility;
- backend/internal/catalogpublic/repository_integration_test.go
  - added TestPublishedOnlyHidesSuspendedAndRetiredCourses;
- backend/internal/httpapi/media_router_test.go
  - included the Course-scoped preview route in the existing uniform anonymous-denial matrix.

No frontend file changed for this tranche.

## 6. Normal preview proof

Real PostgreSQL media fixture:

- Course PUBLISHED;
- current live APPROVED revision;
- valid READY scanner-passed PREVIEW media;
- anonymous/public service calls.

Both IssuePreview and IssueCoursePreview succeeded and issued the current preview asset.
The fixture asserted the existing five-minute signature lifetime and observed two signer calls,
one per allowed entry point.

## 7. Suspension proof

The same real PostgreSQL fixture was mutated to set access_suspended_at.

Observed:

- IssuePreview denied with ErrProtectedUnavailable;
- IssueCoursePreview denied with ErrProtectedUnavailable;
- no signed URL was returned;
- storage signing was not called;
- no suspension timestamp or reason crossed the public service error boundary.

The public HTTP denial matrix maps the ineligible Course-scoped request to the same fixed
not-found response as other unavailable public media.

## 8. Restore proof

The fixture cleared access_suspended_at and access_suspension_reason without changing the live
revision or preview media.

Observed:

- both public preview entry points succeeded again;
- the current preview asset was signed;
- the five-minute lifetime remained unchanged.

This proves dynamic evaluation of current Course state rather than permanent takedown state.

## 9. Retirement proof

The fixture set retired_at while lifecycle remained PUBLISHED.

Observed:

- both public preview entry points denied;
- no new signed URL/token was issued;
- no storage signing call occurred.

Existing protected Student retirement semantics were not changed.

## 10. Delisted and archived proof

The same PostgreSQL fixture then set lifecycle to DELISTED and ARCHIVED.

Observed for each state:

- IssuePreview denied;
- IssueCoursePreview denied;
- no signer call occurred.

The existing non-PUBLISHED behavior remains intact.

## 11. Public catalogue parity

TestPublishedOnlyHidesSuspendedAndRetiredCourses ran against real PostgreSQL migrations and
public catalogpublic.Repository.Detail:

- PUBLISHED and unsuspended/unretired: visible;
- access_suspended_at set: hidden;
- suspension cleared: visible again;
- retired_at set: hidden.

The catalogue and preview now share the same suspension/retirement-aware public eligibility base
predicate. Existing catalogue ranking and academic discovery logic were not changed.

## 12. Protected Student regression

Existing protected-delivery tests passed unchanged, including:

- normal entitled Student playback using the exact READY version;
- Course emergency suspension denial;
- retirement/ineligibility denial;
- current material entry re-checks after Course suspension and retirement;
- entitlement, enrollment, and progress immutability during public preview issuance.

No protected Student playback or Entitlement behavior was changed.

## 13. Signed preview URL residual window

The source config is:

~~~text
PLAYBACK_URL_EXPIRY -> default 5 minutes
DeliveryOptions.SignatureLifetime -> used by public preview signing
~~~

The fixture and existing delivery tests observe 5 minutes. No TTL was lengthened or changed.

Already-issued signed preview URLs are not actively revoked by this tranche. They can remain usable
until their existing short expiry; with the current source default, the maximum residual window is
5 minutes from issuance. New issuance after suspension or retirement is denied immediately by the
database-backed predicate.

## 14. HTTP and error semantics

The existing public media handler maps ErrProtectedUnavailable through writeProtectedUnavailable
to the canonical public not-found response:

- HTTP 404;
- Cache-Control: no-store;
- no redirect;
- no lifecycle state, suspension timestamp, retirement timestamp, reason, SQL, table name, or
  internal media identifier.

The existing byte-identical anonymous-denial test now includes
GET /api/v1/media/courses/:courseID/preview.

## 15. Database and query safety

- No extra preflight query was added.
- The existing preview lookup now applies the canonical predicate in the same query that resolves
  the live revision and preview media.
- Existing parameterized target predicates remain intact.
- Approved/live revision, scanner provenance, READY state, media kind/content type, visibility,
  preview lineage, media retirement, signing, expiry, and exact asset binding remain unchanged.
- No migration or index was added.
- The existing launch-scale public catalogue query-plan test passed.
- No retained database, Docker volume, or production data was touched.

## 16. Tests

Passed:

~~~text
cd backend && go test ./internal/media ./internal/catalogpublic ./internal/httpapi -count=1

cd backend && go test -tags=integration ./internal/media -run '^TestMED01PublicPreviewFollowsCanonicalCourseEligibility$' -count=1

cd backend && go test -tags=integration ./internal/catalogpublic -run '^TestPublishedOnlyHidesSuspendedAndRetiredCourses$' -count=1

cd backend && go test -tags=integration ./internal/media -run '^Test(D8ProtectedDeliveryUsesExactReadyVersionAndPerRequestEvaluation|D064StableMaterialEntryResolvesCurrentVersionAndRechecksAuthority|D8ProtectedDeliveryCollapsesEveryEvaluatorDenial|D8PublicPreviewIsExactPublishedReadyAndPrivateOutsideSigning|MED01PublicPreviewFollowsCanonicalCourseEligibility)$' -count=1

cd backend && go test -tags=integration ./internal/catalogpublic -run '^Test(DetailSecondaryReadsRecheckPublishedOnly|PublishedOnlyHidesSuspendedAndRetiredCourses|PublicProjectionExposesOnlyReadyLiveRevisionPreview)$' -count=1

cd backend && go test ./internal/httpapi -run '^Test(CourseScopedPreviewResponseDoesNotSerializeTheAssetVersion|D8ProtectedDeliveryDenialsAreByteIdenticalOnTheProductionRouter)$' -count=1

cd backend && go test -tags=integration ./internal/httpapi -run '^TestPublicCatalogQueryPlansAtLaunchScale$' -count=1

cd backend && go build ./...
cd backend && go vet ./...
cd backend && go vet -tags=integration ./...
cd backend && go test ./... -count=1
~~~

All commands above passed.

The combined relevant integration command also ran:

~~~text
cd backend && go test -tags=integration ./internal/media ./internal/catalogpublic ./internal/httpapi -count=1
~~~

catalogpublic and httpapi passed. The media package reported one unrelated pre-existing failure:
TestD7MigrationContainsMediaAndEntitlementInvariants expected schema version 26 but the current
dirty worktree migrated the disposable test database to version 27. The MED-01-focused media
tests passed independently.

## 17. Backend gates

- go build ./...: PASS
- go vet ./...: PASS
- go vet -tags=integration ./...: PASS
- go test ./... -count=1: PASS
- relevant focused integration gates: PASS
- full relevant integration set: PARTIAL because of the pre-existing schema 27 versus 26
  assertion described above.

## 18. Canonical E2E

Accepted before-tranche baseline:

~~~text
168 passed
0 failed
0 skipped
~~~

The required current command was run:

~~~text
cd frontend && npm run test:e2e:canonical
~~~

The current result was:

~~~text
production: 0 passed, 0 failed, 0 skipped
development: 0 passed, 0 failed, 0 skipped
aggregate: 0 passed, 0 failed, 0 skipped
exit 1
~~~

Neither lane reached a test because the run-owned API readiness probe returned 503:
schema version is not supported by this build: found 27, this build supports 14..26.
The run-owned worker also exited during email_schema_check. This is the existing protected dirty
worktree migration mismatch, not a MED-01 assertion failure. No canonical test was added or skipped.

## 19. Files changed by this tranche

- backend/internal/media/delivery.go
- backend/internal/media/delivery_integration_test.go
- backend/internal/catalogpublic/repository_integration_test.go
- backend/internal/httpapi/media_router_test.go
- docs/launch/evidence/2026-08-24-med-01-public-preview-takedown.md

The immutable review file docs/reviews/2026-08-24-ox-alpha-full-repository-review.md was not edited.
All unrelated working-tree changes were preserved.

## 20. Evidence

This record is the remediation evidence:
docs/launch/evidence/2026-08-24-med-01-public-preview-takedown.md.

The source predicate is verified in backend/internal/catalogpublic/visibility.go and its unit
test. The preview query and signer lifetime are verified in backend/internal/media/delivery.go.
The public HTTP contract is verified in backend/internal/httpapi/media_delivery_handlers.go and
media_router_test.go.

## 21. Ox Alpha status

MED-01: **OPEN -> CLOSED** on focused remediation proof.

The historical Ox Alpha review remains immutable. The unrelated canonical E2E/release-gate mismatch
does not reopen the MED-01 finding.

## 22. MVP tracker

Old: 45 / 53 = 84.9%
New: 45 / 53 = 84.9%

MED-01 is an audit/security finding, not a counted MVP row. No tracker row was changed.

## 23. Paid-beta security status

- Payment NULL-expiry remediation: CLOSED.
- INF-01: IMPLEMENTED, REAL OFFSITE PROOF PENDING.
- MED-04: CLOSED.
- MED-05: CLOSED.
- MED-01: CLOSED on focused remediation proof.

## 24. Repository safety

- No git reset, clean, stash, restore, broad checkout, or repo-wide formatting.
- No retained database or Docker volume was dropped, truncated, or removed.
- Only disposable integration databases owned by existing test fixtures were used.
- No unrelated process was killed.
- No deployment was performed.
- git diff --check was clean after the implementation edits.

## 25. Remaining security and production work

- Reconcile the existing migration 0027/API-worker schema-version mismatch so the canonical E2E
  release gate can run.
- INF-01 real offsite-provider and restore proof.
- Separately authorized MED-02 playback-authorization rate limiting.
- MED-03 security headers, MED-06 Redis TLS renewal, MED-07 stuck media processing recovery, and
  remaining LOW findings.

MED-04 and MED-05 are closed and are not remaining work. No MED-02/MED-03/MED-06/INF-01/R2/GAP-06
implementation was started here.

## 26. Recommended next step

Resolve the pre-existing schema-27 release-gate mismatch under its own authority, then proceed with
the separately authorized MED-02 duplicate playback-authorization rate-limiting tranche.

## 27. Final status line

**MED-01 PARTIAL — remediation proof complete; canonical E2E blocked by pre-existing schema 27 mismatch.**
