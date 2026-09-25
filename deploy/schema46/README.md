# Schema-46 rollback-compatible application artifact

An application rollback path for the schema 44 → 46 release, with no destructive
database downgrade.

## The problem

Production runs `0fee657897c939cb679c9d804d184542bb2f692f` at clean schema 44.
That binary's `MaxSchemaVersion` is 44, so the moment 0045 and 0046 are applied it
refuses to start — its supported range no longer covers the database. The release
would therefore have **no application rollback at all**, and the only way back
would be a destructive downgrade.

There is deliberately no generic production `DOWN`, and no supervised 46 → 44
command. So the rollback path is an application rollback onto a database that
stays at 46.

## What the artifact is

**`0fee657` behaviour, plus a schema-46 compatibility patch.** It is not the
original `0fee657` artifact and must never be labelled as one.

| | |
|---|---|
| Base commit | `0fee657897c939cb679c9d804d184542bb2f692f` |
| Patch | [`rollback-compat.patch`](rollback-compat.patch) |
| Patch SHA-256 | `f2d3c6bb0294c4b5982c035529b0341d8c5b1fc346ad85a80765d5e73d1405b3` |
| Supported schema range | **44 .. 46** |
| Compatibility ceiling | schema 46 |
| API readiness floor | schema 44 — **corrected from `0fee657`'s 43** |
| Worker media floor | schema 42 — unchanged |
| Runs automatic enhancement recovery | no |
| Serves Lesson public preview | no |
| Serves legacy course-level preview | yes, exactly as today |

The patch changes nine files and nothing else:

- the four 0045/0046 migration files, byte-identical to the current tree, so the
  migrator source matches the database the artifact will serve;
- `internal/db/schema.go` — the ceiling and the two named constants;
- `internal/db/schema_test.go` — the assertions for them;
- `cmd/api/main.go` — the corrected readiness floor;
- `cmd/api/main_test.go` — the floor assertion, corrected and extended;
- `cmd/migrate/main.go` — the compiled `schema-range` release-selection command;
- `cmd/schema46-rollback-probe/main.go` — new, integration-tagged.

### The API readiness floor is 44, not 43

`requiredSchemaVersion` is `DirectPurchaseAccessGrantSchemaVersion` (44).

An earlier revision of this artifact left it at `0fee657`'s 43 and argued the
difference could not matter, because the database being rolled back onto is at
46 and satisfies either floor. **That argument was wrong.**
Release/application selection checks a *maximum* supported schema and no
minimum, so nothing prevented this artifact from being selected on a schema-43
database, reporting ready, and then failing at runtime.

It would fail because this build carries the direct Course grant, and that
behaviour genuinely requires schema 44. Migration 0044 drops
`ent_purchase_needs_invitation` and replaces it with `ent_purchase_source_valid`;
below 44 the old constraint still demands `source_invitation_id IS NOT NULL`, so
a direct grant — which writes `source_purchase_request_id` and no invitation — is
refused by the database at write time. Readiness that passes while those writes
cannot succeed is untruthful readiness.

Raising the floor is therefore a **truthfulness correction to the artifact's
declared supported range**, not forward behaviour cherry-picked into a rollback
target. The artifact's behaviour is unchanged; only its self-description is.

`0fee657`'s own floor assertion had additionally drifted — it still named schema
38 while the build required 43 — and the verification script ran `cmd/api`'s
tests without the `integration` tag, so the assertion was never executed. Both
are corrected, and the script now names that test explicitly.

**The truthful supported range is 44..46.** The artifact serves 44, 45 and 46,
and refuses 43 and below. The refusal is proven, not asserted: see step 8 of the
verification script.

No other forward behaviour is included. The artifact runs no scheduler, exposes
no Lesson preview, and keeps the existing access, device and media behaviour.

## Why tolerating schema 45 and 46 is safe

Both migrations are purely additive, and this build reads neither addition.

**0045** creates `media_auto_enhancement_recovery` and adds a nullable
`processing_attempts.auto_recovery_intent_id`. Every statement the artifact
issues against `processing_attempts` names its columns explicitly, so the extra
nullable column is invisible to it, and it never touches the new table. It
therefore runs no automatic recovery — which is the point of rolling back to it.

**0046** adds `course_lessons.allow_public_preview`, `NOT NULL DEFAULT false`. The
artifact's Lesson reads and its revision clone both name their columns, so the
default carries every row it writes, and it has no Lesson-preview behaviour to
expose. It serves the legacy course-level preview through
`course_revisions.preview_asset_version_id`, which 0046 leaves untouched.

That claim is not taken on trust. `cmd/schema46-rollback-probe` runs every
statement shape the previous build actually issues, against a real schema-46
database.

## Verifying

```bash
deploy/schema46/verify-rollback-compat.sh
```

It checks the patch checksum, confirms the base commit is present, reproduces the
artifact tree, proves the migration SQL is byte-identical to the current tree's,
runs the artifact's own tests, migrates a disposable database to 46 with the
**current release's** migrations, and then runs the probe against it. The probe
covers:

- API and worker readiness at schema 46
- the schema-45/46 objects present and ignored — absent would mean the probe ran
  against the wrong database and proved nothing
- a processing attempt written without the attribution column
- a canonical rendition and the FULL media assumptions around it
- the Lesson read, naming its columns
- the revision clone, which writes `course_lessons` without mentioning the new
  flag and must get `false` from the default
- the legacy course-level preview resolution, with all its predicates
- the direct Course access grant — an Entitlement with a purchase request and no
  invitation, and a COURSE request granted with no invitation
- the negative: the artifact writes no scheduler row and marks no Lesson
  previewable

It also migrates a second disposable database to clean schema 43 and proves the
artifact **refuses** it, which is the negative half of the 44..46 range claim.

It also proves the rollback image's compiled `schema-range` output is `44 46`.
It creates and drops two disposable databases and touches nothing else.

## What this is not

**It is not a substitute for fresh schema-44 backup and proven restore
evidence.** That remains mandatory before any production deployment of this
release. This artifact proves an application rollback is possible; it proves
nothing about data recovery.

It has also not been built as an image, imported, or deployed. Nothing in this
directory has run against production.

## Immutable local artifact

`build-rollback-artifact.sh OUTPUT_PARENT` reproduces the base-plus-patch tree,
builds backend, proof, and frontend images under a derived release identity, and
writes checksummed image and manifest metadata. The identity is derived from the
base SHA and patch SHA, so it can never be confused with either `0fee657` or the
forward candidate. This is a local staging operation only; importing that
artifact to a production host remains a later, reviewed release action.
