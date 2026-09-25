# Schema 44 to 46 release boundary

This release was executed in production on 2026-09-25. Frozen release
`0c5c67e20f2bbf7312e403889aa1f021adb8748a` is running on clean schema 46 with
3C-C off. What follows is the procedure as it now stands, corrected against what
the deployment actually did.

## The named boundary

The named command is `up-core-schema-46-media-preview`. It accepts only the
`SCHEMA46_CAPABILITY=auto-enhancement-lesson-preview-v1` bundle and starts only
from `44|false`. It verifies immutable candidate images and bundle contents,
their compiled schema range `44..46`, migration hashes for 0045 and 0046, fresh
schema-44 backup/restore evidence, the schema-46 old-behaviour rollback artifact,
removed API/worker producers, zero active media claims, PostgreSQL/Redis health,
and `MEDIA_AUTO_ENHANCEMENT_RECOVERY_ENABLED=false`.

The command applies 0045, verifies `45|false`, then applies 0046 and verifies
`46|false` before starting the candidate. It never uses generic production down,
force, or marker repair. A dirty marker after either step means no application is
selected automatically; preserve the actual marker and make a recovery decision.

## Operator flow

```
import candidate            # import-release.sh CANDIDATE_SHA
import rollback artifact    # import-release.sh ROLLBACK_SHA
select the pair             # host.sh select-release CANDIDATE_SHA ROLLBACK_SHA
inspect the selection       # host.sh show-release-selection
run the boundary            # host.sh up-core-schema-46-media-preview
```

`select-release` writes only the release-selection keys in `runtime.env` —
`GRADEX_RELEASE_SHA`, the three image keys, and
`GRADEX_SCHEMA46_ROLLBACK_RELEASE_SHA` — by atomic rename, and starts, stops,
migrates and enables nothing. It refuses a candidate whose bundle, manifest,
images, capability or compiled range do not agree, and refuses a schema46
candidate offered without its rollback artifact, so the pair cannot be left
half-selected. A refused selection leaves `runtime.env` byte-identical.

Selection is not permission to weaken anything: the boundary independently
re-verifies every identity afterwards.

> The 2026-09-25 deployment had no such command. Release selection was done by
> hand-editing `runtime.env`, including inserting the rollback release identity,
> immediately before the boundary ran. That is recorded here because the fix
> exists to stop it being repeated, not because it is the procedure.

## Rollback

If the candidate cannot start or a post-start smoke fails on clean schema 46,
select the separately imported schema46 rollback-compatible old-behaviour
artifact through application release selection. Its compiled range is `44..46`;
no database down runs. It retains legacy previews, direct Course grants, device
behaviour and protected playback, and has no automatic recovery scheduler or
Lesson-preview route.

The rollback artifact is built by `deploy/schema46/build-rollback-artifact.sh`
and imported by the ordinary `import-release.sh`. It stages its tooling from the
shared release closure in `deploy/hostinger/release-closure.sh`, so its bundle
has the same layout as the forward candidate's and needs no special handling.

> The 2026-09-25 deployment could not import it. The builder staged tooling with
> `cp -R deploy/hostinger "$TOOLING/deploy"`, flattening `deploy/hostinger/*`
> onto `deploy/*`, and the importer — which sources
> `tooling/deploy/hostinger/release-artifact.sh` out of the artifact — failed
> outright. The artifact was staged by a hand-written mirror of the importer's
> verification logic. `deploy/schema46/verify-rollback-artifact-import.sh` now
> builds a real artifact and imports it with the unmodified importer, and fails
> if that layout regresses.

## What the disposable drill actually proves

`deploy/schema46/verify-application-rollback.sh` is the application-selection
drill. It is **disposable local release-tooling evidence, not production
evidence.** It runs in its own Compose project with its own volumes and
generated secrets, and never touches production PostgreSQL, Redis or storage.

It switches real containers against one PostgreSQL instance, one database and
one data directory, with no recreation, restore or reseed between switches:

```
real candidate API + worker
  -> real rollback-compatible API + worker
  -> real candidate API + worker
```

The schema stays `46|false` across every switch. Along the way it establishes
clean 44 with the previous release's own migrator, migrates 44 → 45 → 46 with the
candidate's targeted commands, and proves:

- candidate API health and readiness
- rollback API health and readiness
- worker liveness on both applications
- direct Course access grant and the resulting entitlement
- Student protected playback authorization
- legacy course-level preview resolution
- Lesson Preview **master** manifest on the candidate
- candidate restart on the same data
- candidate-failure → rollback recovery
- stable database identity across the whole cycle
- the schema43 refusal, which is the negative half of the 44..46 range claim

It also runs the extracted no-Git dual-artifact boundary and the pinned rollback
compatibility proof.

### Where the drill stops

The drill follows Lesson Preview only as far as the **master manifest**. Its
fixture does not upload HLS rendition playlists or segment objects to MinIO, so
it does not fetch a rendition playlist and does not fetch or decode a segment.

That is a limit of this drill, not a gap in coverage. Final G0 separately
verified browser E2E coverage for master manifest → rendition manifest → real
segment and decoder configuration. Do not read the drill's stopping point as
evidence that the rendition or segment path is untested.

## 3C-C stays off

3C-C remained off throughout the release and remains off now. Enabling it later
requires one legitimate operational manual 3C-B observation in production; test
or release smoke evidence is not that observation. As of the 2026-09-25
deployment, production carries zero `ENHANCEMENT` and zero `FINALIZATION`
processing attempts, so that observation has not occurred.

`MEDIA_AUTO_ENHANCEMENT_RECOVERY_ENABLED` is **not currently passed through the
Compose backend or worker environment.** For this release that was fail-safe,
because the flag had to stay false and an unset flag is false. A future 3C-C
activation tranche must add that wiring deliberately *and* satisfy the
production 3C-B observation gate first. Do not wire it before then: wiring it
early creates a flag that looks operable while the gate it depends on is unmet.

## Production smoke fixture — outstanding

The deployment could not run positive production smoke for Student playback,
direct Course access, or Lesson Preview, because production has no approved
smoke identity or content fixture: 21 accounts, none matching a smoke/e2e/test
fixture, and zero `fake_entitlements`. Nothing was manufactured to close that —
creating a customer account, Course or Entitlement to satisfy a smoke would put
fabricated business data in production.

A future tranche should define an approved, clearly non-customer production
smoke fixture policy: how such an identity and its content are created,
segregated from real catalogue and billing data, audited, and retired. Until it
exists, those three smokes are correctly reported as NOT AVAILABLE rather than
skipped or faked.
