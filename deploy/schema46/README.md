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
| Patch SHA-256 | `290ececcd67842996ca5007f8c0d101f9a9dbe972c6479f230fb883505093f4b` |
| Compatibility ceiling | schema 46 |
| API readiness floor | schema 43 — **unchanged from `0fee657`** |
| Worker media floor | schema 42 — unchanged |
| Runs automatic enhancement recovery | no |
| Serves Lesson public preview | no |
| Serves legacy course-level preview | yes, exactly as today |

The patch changes seven files and nothing else:

- the four 0045/0046 migration files, byte-identical to the current tree, so the
  migrator source matches the database the artifact will serve;
- `internal/db/schema.go` — the ceiling and the two named constants;
- `internal/db/schema_test.go` — the assertions for them;
- `cmd/schema46-rollback-probe/main.go` — new, integration-tagged.

### What is deliberately not cherry-picked

`requiredSchemaVersion` stays at `AutoDeviceReplacementSchemaVersion` (43).
Raising it to 44 is a correctness fix that belongs to the forward release, not a
compatibility change, and pulling it in here would make this artifact something
other than `0fee657` behaviour. It cannot matter for a rollback target anyway:
the database being rolled back onto is at 46, which satisfies either floor.

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

It creates and drops one disposable database and touches nothing else.

## What this is not

**It is not a substitute for fresh schema-44 backup and proven restore
evidence.** That remains mandatory before any production deployment of this
release. This artifact proves an application rollback is possible; it proves
nothing about data recovery.

It has also not been built as an image, imported, or deployed. Nothing in this
directory has run against production.
