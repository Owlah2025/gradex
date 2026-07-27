# Feature Specification: S4 — Media Pipeline, Protected Delivery, and Entitlement Evaluation

**Feature Branch**: `feature/005-media-and-entitlement-evaluation`

**Created**: 2026-07-28

**Status**: Draft — frozen for D7–D9 implementation under [D-040](../../docs/DECISIONS.md#d-040--august-15-restored-as-the-hard-mvp-launch-date-claude-plans-antigravity-implements-claude-reviews)

**Input**: S4 — direct upload, validation, quarantine, malware-scan adapter, transcode pipeline,
Asset Versions, short-lived signed media access, protected Resource and Lab Material downloads, and
**Entitlement evaluation**: the grant record, scope evaluation, expiry, and revocation.

**Depends on**: S1C (closed) and S2. **S4 implementation does not begin until S2 closes** on an
independent verdict.

**Effort**: 18h — the largest slice in the release. **Review Tier 3** — the deepest tier, shared only
with S1C, S5, S6, and S7.

**Governing rules**: BR-021, BR-023, BR-024, BR-025, BR-026, BR-027, BR-028, BR-050, BR-063, BR-090,
BR-100, BR-103, BR-104, BR-143, BR-144. Traceability is carried per requirement.

---

## The two boundaries this slice exists to hold

Everything else in S4 is pipeline plumbing. These two are why it is Tier 3, and both are **ordering**
decisions that look wrong until you see what they prevent.

### 1. S4 evaluates Entitlements. It must never create one.

Entitlement *creation* from verified payment is **S7**. S4 owns the grant record, scope evaluation,
expiry, and revocation — the consumer side.
[SLICES.md §3.1](../../docs/launch/SLICES.md#31-entitlement-evaluation-precedes-entitlement-creation)
calls this *"the single most load-bearing ordering decision in the register"*, because getting it
wrong pushes access-control verification into payment implementation, where it gets far less
scrutiny under far more schedule pressure.

The production invariant is unchanged by the split:

> Every real Entitlement originates from a completed paid or zero-value Coupon Order Item, except
> reconciliation that restores one from an already valid completed Order Item. *(BR-028)*

**S4 therefore ships no manual-grant command, no Admin grant screen, and no runtime flag that could
mint an Entitlement.** Its test path is a seed mechanism that is **build- or environment-excluded from
production images**, not merely disabled by configuration — the same standard the `AUTH_FAKE_MODE`
seam is held to. A configuration flag that *could* be flipped in production is not excluded from
production.

### 2. Nothing unscanned is ever reachable.

`LG-014` — the malware scanner — is **unresolved and may remain so at launch**. S4 is therefore built
so that the unresolved gate degrades **availability**, never **safety**:

> Public previews and downloadable assets are unavailable until malware scanning succeeds; a failed or
> unavailable scan fails closed and leaves the asset unavailable. *(BR-104)*

An asset whose scan has not succeeded is indistinguishable from an asset that does not exist. Scan
failure, scanner outage, scanner misconfiguration, and scanner absence all produce the same outcome:
**not available**. If the scanner cannot be reached, uploads queue as unscanned and stay unreachable —
they do not pass through with a warning.

The documented fallback for an unresolved `LG-014` at launch is Admin-only catalogue upload with
out-of-band scanning ([§2.4](../../docs/launch/AUGUST_15_EXECUTION_PLAN.md#24-launch-gates-on-the-critical-path)).
That is an **operating mode with a switch S4 builds deliberately**, not an improvisation invented on
August 13.

---

## Scope Boundaries

| S4 owns | S4 must not acquire |
|---|---|
| Media **bytes**: upload, validation, quarantine, scan, transcode, Asset Versions | Course/Section/Lesson structure and metadata — S2 ([§3.2](../../docs/launch/SLICES.md#32-authoring-owns-media-metadata-the-media-slice-owns-media-bytes)) |
| Entitlement **evaluation**: grant record, scope, expiry, revocation | Entitlement **creation** from payment — **S7** |
| Short-lived signed access to protected media | The player, progress, completion, resume — S5 |
| Protected Resource and Lab Material downloads | Orders, checkout, coupons, refunds — S6/S7 |
| Cutover of the legacy `internal/video` path | A second media authority alongside it |

**The legacy video path is migration input, not a second authority.** `backend/internal/video`
currently enqueues transcoding directly to asynq **with no scan step and no outbox**. S4 cuts it over
forward-only ([§3.3](../../docs/launch/SLICES.md#33-the-legacy-video-path-is-migration-input-not-a-second-authority),
D-031). Leaving it running beside the new pipeline would mean two ways to publish bytes, one of which
never scans anything.

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 — An Instructor uploads a video and it becomes playable (Priority: P1)

**Acceptance**
1. Upload of a valid video is accepted, quarantined, scanned, transcoded to HLS, and produces an
   Asset Version reachable by S2's reference.
2. At **no point** before scan success is the object reachable through any delivery route.
3. A failed scan leaves the asset permanently unavailable, with the failure visible to the Instructor
   and to an Admin, and retryable by an Admin.
4. A **scanner outage** produces the same unavailability as a failed scan — not a pass, not a warning.
5. Duplicate completion callbacks are harmless; the same upload does not produce two Asset Versions.

### User Story 2 — An entitled Student plays a protected Lesson (Priority: P1)

**Acceptance**
1. A Student with an active Course Entitlement receives short-lived, session-scoped signed access to
   the exact approved Asset Version *(BR-050, BR-100)*.
2. A Student with a **Section** Entitlement reaches Lessons in that Section and **no others**
   *(BR-024)*.
3. A Student with a **Course** Entitlement reaches every contained Section *(BR-024)*.
4. Access after `access_ends_at` is denied *(BR-025)*.
5. Access to a Course under **emergency access suspension** is denied immediately, without mutating
   any Entitlement *(BR-090)*.
6. A revoked Entitlement denies access; the Enrollment and Progress rows survive *(BR-047)*.
7. Every denial is identical regardless of whether the file exists *(BR-023, BR-050)*.

### User Story 3 — An entitled Student downloads Resources and Lab Materials (Priority: P1)

**Acceptance**
1. Download is entitlement-checked **per download**, not once per session *(BR-023, BR-063)*.
2. A Lab Material carries an opaque per-purchase buyer tag; a Lesson Resource does **not**
   *(BR-103)*.
3. The buyer tag exposes no Student PII and is not reversible by its holder *(BR-103)*.
4. An unscanned or scan-failed attachment is not downloadable *(BR-104)*.

### User Story 4 — A visitor plays a public preview (Priority: P2)

**Acceptance**
1. A public preview is playable without authentication, **after** validation, quarantine, scan
   success, and Instructor publication confirmation *(BR-144)*.
2. No protected Lesson video, Resource, or Lab Material is reachable from the preview path
   *(BR-143)*.

### Edge Cases

- Entitlement expiring **mid-playback**: issued signed access remains valid for its short lifetime;
  no new access is issued. Access is not retroactively revoked mid-segment, and the window is bounded
  by the signature lifetime rather than by the session.
- A Student holding **both** a Section and a Course Entitlement: access is their union, both survive
  independently, and revoking the Course grant leaves the Section grant intact *(BR-024)*.
- A **retired** Asset Version still reachable through a grandfathered Entitlement whose
  `retirement_eligibility_at` precedes `retired_at` *(BR-027)*.
- Scanner returns success for an object that was **replaced** after scanning — the scan binds to an
  exact object version, not to a logical asset.
- Transcode succeeds but produces zero renditions — treated as failure, not as an empty success.
- An Admin shortens `access_ends_at` into the past: access ends immediately; Enrollment, Progress,
  Order, and adjustment history are preserved *(BR-026)*.

---

## Requirements *(mandatory)*

### Functional Requirements

**Upload, validation, quarantine**

- **FR-001**: System MUST accept direct upload of media to quarantine storage, validated on type and
  size before acceptance. *(BR-104)*
- **FR-002**: System MUST keep every uploaded object unreachable by any delivery route until scan
  success. *(BR-104)*
- **FR-003**: System MUST treat scan failure, scanner error, scanner timeout, and scanner
  unavailability identically: the asset remains unavailable. *(BR-104)*
- **FR-004**: System MUST bind a scan result to an **exact stored object version**, so that replacing
  an object invalidates its prior scan result.
- **FR-005**: System MUST expose scan and processing state to the owning Instructor and to an Admin,
  and MUST allow an Admin to retry a failed scan or transcode.

**Asset Versions and transcode**

- **FR-006**: System MUST produce immutable Asset Versions; a new upload creates a new version and
  never mutates an existing one.
- **FR-007**: System MUST transcode video to HLS and record the **trusted duration** of the exact
  Asset Version, which S5 depends on for completion calculation. *(BR-051)*
- **FR-008**: System MUST treat a transcode producing zero renditions as a failure.
- **FR-009**: System MUST make upload-completion and transcode-completion handling **idempotent**;
  duplicate, delayed, and out-of-order callbacks MUST NOT create duplicate Asset Versions.
- **FR-010**: System MUST route all processing through the durable queue and outbox boundary, and MUST
  retire the legacy direct-to-asynq path rather than leaving it available. *(SLICES §3.3, D-031)*

**Entitlement evaluation — and only evaluation**

- **FR-011**: System MUST provide an Entitlement grant record carrying scope (Course or Section),
  `original_access_ends_at`, effective `access_ends_at`, revocation state, and its source Order
  reference. *(BR-021, BR-026, BR-028)*
- **FR-012**: System MUST evaluate a Course Entitlement as covering every contained Section, and a
  Section Entitlement as covering only its Section. *(BR-024)*
- **FR-013**: System MUST evaluate overlapping Entitlements as a **union**, keep them independent, and
  MUST NOT let revocation of one affect another. *(BR-024)*
- **FR-014**: System MUST deny access when `current_timestamp >= access_ends_at`, treating the
  Entitlement as authoritative at runtime. *(BR-025)*
- **FR-015**: System MUST deny access to a Course under active emergency access suspension **without
  mutating any Entitlement**. *(BR-090)*
- **FR-016**: System MUST allow access to a retired Course, Section, Lesson, or version only when
  `retirement_eligibility_at` precedes `retired_at`. *(BR-027)*
- **FR-017**: System MUST NOT provide any path — command, endpoint, screen, or configuration flag —
  that creates an Entitlement. *(BR-028, SLICES §3.1)*
- **FR-018**: The S4 Entitlement seed mechanism used for testing MUST be **build- or
  environment-excluded from production images**, not merely disabled by configuration. *(SLICES §3.1)*

**Protected delivery**

- **FR-019**: System MUST check Entitlement **before every** signed playback issuance and **every**
  protected download. *(BR-023)*
- **FR-020**: System MUST issue playback access as short-lived, session-scoped signed URLs, re-issued
  per playback session and not cached long-term. *(BR-100)*
- **FR-021**: System MUST authorize playback only for the exact approved or historically qualifying
  Asset Version. *(BR-050)*
- **FR-022**: System MUST deny unauthorized callers identically regardless of whether the underlying
  file exists. *(BR-023, BR-050)*
- **FR-023**: System MUST apply an opaque per-purchase buyer tag to **Lab Materials only**, never to
  Lesson Resources, and the tag MUST NOT expose Student PII. *(BR-103)*
- **FR-024**: System MUST keep media objects private; **no object is publicly readable**, and every
  access is signed and time-bounded. *(PRD §6)*
- **FR-025**: A redirect MUST NOT grant access. Authorization is server-side on every issuance.

**Operating mode for an unresolved `LG-014`**

- **FR-026**: System MUST support a documented operating mode in which public upload is disabled and
  only an Admin loads catalogue assets, with scanning performed out of band and recorded. This is a
  deliberate switch, not a code change made under launch pressure. *(§2.4)*

### Key Entities

| Entity | Owner | S4's relationship |
|---|---|---|
| Asset Version | **S4** | Created and made immutable here |
| Scan Result | **S4** | Bound to an exact object version |
| Entitlement | **S4 evaluates**, S7 creates | Grant record, scope, expiry, revocation |
| Course / Section / Lesson | S2 | Read only |
| Order | S6/S7 | Referenced by Entitlement; **not created here** |

---

## Success Criteria *(mandatory)*

- **SC-001**: Across every failure mode of the scanner — failure, error, timeout, absence,
  misconfiguration — **zero** assets become reachable. Proven per mode, not in aggregate.
- **SC-002**: No route, command, flag, or fixture in a **production build** can create an Entitlement.
  Proven by a test asserting the seed mechanism is absent from a production build, not merely off.
- **SC-003**: A Section Entitlement reaches exactly its Section's Lessons and no others, enumerated
  across the full Course graph rather than sampled.
- **SC-004**: Expired, revoked, suspended-Account, and emergency-suspended access are each denied, and
  each denial is byte-identical to the denial for a non-existent asset.
- **SC-005**: Duplicate and out-of-order upload/transcode callbacks produce exactly one Asset Version.
- **SC-006**: No media object is publicly readable; verified by direct unsigned request to storage.
- **SC-007**: Lab Materials carry a buyer tag, Lesson Resources do not, and the tag contains no
  Student PII — asserted on the tag's bytes.
- **SC-008**: The legacy `internal/video` direct-to-asynq path no longer exists as a reachable code
  path.

---

## Assumptions

- S2 has closed, so Asset Version **references** exist and are validated by authoring.
- `LG-014` may be unresolved at launch. FR-026 is the planned response, not a contingency.
- Object storage is S3-compatible with presigned URL support (MinIO locally, provider at launch).
- S7 will attach real Entitlement creation to this already-proven consumer. **S4 does not stub S7** —
  it defines the record and evaluates it; nothing here anticipates the producer's transaction.

## Dependencies

| Depends on | For | State |
|---|---|---|
| S2 | Course graph, Asset Version references, emergency suspension state | **Blocking** |
| S1C | Capability gate, session, ownership precondition | Closed |
| `LG-014` | Production scanner | **Unresolved** — FR-003 and FR-026 make that survivable |
| S7 | Entitlement creation | **Deliberately absent.** S4 must remain complete without it |
