# Student device security — trusted devices and single protected playback

**Status:** REMEDIATION APPROVED 2026-09-11; FINAL VERIFICATION PENDING.
**Branch:** `feature/student-device-security`. **Base:** `ef733f758cf7d58e2689898a70715e2fd2102e24`.
**Decision record:** [D-105](../../DECISIONS.md#d-105--student-device-trust-and-single-protected-playback-are-student-only-controls).
This work is outside the D-089 `MVP-Fxx` queue. An independent reviewer verdict against the final
exact commit range remains a prerequisite to merging or deploying it.

## Policy implemented

| Control | Value | Configuration |
|---|---|---|
| Trusted devices per Student | 2 | `STUDENT_TRUSTED_DEVICE_LIMIT` |
| Replacement cooldown | 24h | `STUDENT_DEVICE_REPLACEMENT_COOLDOWN` |
| Concurrent protected playback per Student | 1 | structural |
| Playback lease TTL | 75s | `STUDENT_PLAYBACK_LEASE_TTL` |
| Player heartbeat interval | 25s | `STUDENT_PLAYBACK_HEARTBEAT_INTERVAL` |

Scope is STUDENT accounts only. Instructor and Admin authentication is untouched by construction:
`identity.AuthorizeSessionDevice` returns a non-Student principal's decision unchanged whatever its
recorded device state, and a test asserts that for every role × trust-state × capability triple.

## Architecture

**Trusted device.** A browser holds one opaque 32-byte CSPRNG credential in `__Host-gradex_device`
(Secure, HttpOnly, SameSite=Strict, Path=/, Max-Age 400 days). The server stores only its SHA-256
digest. The credential authenticates nobody and grants no capability: it answers "which of this
Student's browsers is this", and nothing else. Session authority remains entirely the existing
session-family system.

HttpOnly is deliberate: the frontend never reads the value — every decision that depends on it is
made server-side from the digest — so exposing it to script would buy nothing and would hand any XSS
a copy of a credential that survives sign-out. This is the opposite call from the CSRF token, which
the browser must read and which therefore is not a cookie.

The digest is *not* globally unique. One browser holds one origin-scoped device cookie, so a shared
household browser presents the same digest to two different Student accounts; those are two
independent device records consuming one slot each, which is the honest model.

**Session ↔ device.** `sessions` gains a nullable `trusted_device_id` and a
`device_trust_state` enum (`NOT_APPLICABLE` / `PENDING_DEVICE_TRUST` / `TRUSTED` /
`LEGACY_UNBOUND`). Trust is re-derived from the device record on every session resolution and
requires the request to present the credential digest bound to that record. A copied session cookie
without the matching device cookie receives pending-device authority, not protected Student access.
Revoking a device takes effect on the very next request.

Device revocation revokes exactly the session families carrying that `trusted_device_id`. It never
touches `accounts.session_epoch` — that counter is the global "everything stops now" control behind
logout-all, suspension and password reset, and spending it to remove one browser would sign the
Student out of the browser they are still using.

**Restricted pending session.** A browser that authenticates but is not yet trusted receives a real
session narrowed to exactly `CapDeviceManagement`, `CapPasswordChange` and `CapSessionTerminate`.
This reuses the existing restricted-principal shape (`Principal.Restricted()` and the `Authorize`
precedence chain) rather than inventing a bypass flag. Account-level refusals still win: a suspended
Student on a trusted device is refused for suspension, which is what monitoring must see.

**Rollout.** Every pre-existing session row defaults to `LEGACY_UNBOUND`. Such a session keeps
ordinary authority and loses exactly one capability — `CapLearningAccess` — until the browser adopts
a device. That is the narrowest rule that avoids both a mass logout at deployment and a permanent
exemption from the policy. Adoption binds the family; a browser already holding a live trusted
record is bound without a second emailed code, because the Student proved that exact browser once
already.

**Playback lease.** Redis, key `gradex:playback:lease:<account_id>`, a hash carrying
`account_id`, `device_id`, `lease_id`, `session_id`, `lesson_id`, `issued_at`, `heartbeat_at`, with
a TTL. Every operation is one Lua script executed atomically, using Redis `TIME` as the clock:

| Operation | Semantics |
|---|---|
| acquire | free key → take it; same device → **replace** and report the superseded lease; other device → `CONFLICT` |
| validate | exact account+device+lease must own the key; pure read, never creates, never extends the TTL |
| renew | exact account+device+lease only; refreshes heartbeat and TTL |
| release | exact account+device+lease only; deletes |
| release-device | deletes only if the named device holds it |

The lease id is what makes this safe against its own history: an old tab on the same device holds an
older lease id and can neither renew nor release the newer playback instance.

**Enforcement boundary.** `media.DeliveryService.IssuePlayback` acquires the lease *after* the
approved-target load, the entitlement decision and the watermark read, and binds the lease id into
the signed playback session token (domain bumped to `v3`, so tokens minted before this change — which
describe a playback instance holding no lease — are refused by construction). Both existing playback
entry points converge on that one method. Manifest, heartbeat, and release requests must come from
the same trusted device bound into the signed token; another trusted device of the same Account
cannot replay it. Both manifest endpoints validate the exact lease. The heartbeat re-runs the same
authorization the manifest path does, so it doubles as runtime revalidation of a stream already in
progress. Any heartbeat failure stops the first-party player.

Redis failure is fail-closed everywhere: `ErrPlaybackCoordinationUnavailable` → 503. There is no
process-local fallback and no "allow playback if Redis is down" branch.

## Documented residual — already-presigned HLS segments

`rewriteMediaPlaylist` presigns every segment URL up front, expiring at the playback-session expiry,
and VOD playlists carry `#EXT-X-ENDLIST` so the player fetches each media playlist once. Option (a)
was chosen deliberately: **do not change the reviewed VOD HLS/presigned-segment architecture in this
feature.**

Therefore, stated precisely:

> Concurrent-playback enforcement is authoritative at playback authorization and manifest
> acquisition, and cooperative thereafter through the first-party player heartbeat. Already-issued
> direct segment presigns cannot currently be server-revoked mid-stream.

This is not cryptographic or DRM-grade instantaneous revocation, and must not be described as such.
What it does establish is that a second device cannot **start** protected playback while the first
holds the lease, which is the account-sharing control the feature exists to provide. Whether to
introduce short-lived or sliding media authorization is a separate decision for a future D-record.

## Security reasoning

- **Stolen session cookie** — unchanged authority, plus the thief must also hold that session's
  device credential to reach protected learning, and cannot start playback while the legitimate
  device holds the lease.
- **Copied device credential / copied browser profile** — a real residual. `__Host-` + HttpOnly +
  Secure + SameSite=Strict means JS cannot read it and no subdomain can set it, so copying requires
  filesystem or profile access. A copied credential impersonates that device; it does not
  authenticate, and it does not lift the one-playback limit.
- **Race between two simultaneous starts** — one Lua script, one winner. Proven with `-race` over 24
  concurrent goroutines and across two independent Redis clients.
- **Replayed playback authorization** — a stale lease id fails validation and cannot recreate the
  lease. Acquire and validate are deliberately different operations.
- **Two API instances** — no process-local state anywhere; proven with two clients.
- **Stale heartbeat** — the lease lapses on its TTL; nothing needs cleaning up.
- **Device removed / account suspended / entitlement revoked mid-playback** — the next authorization,
  manifest fetch or heartbeat is refused, and the first-party player stops. Segment URLs already
  issued remain fetchable until they expire, per the residual above.
- **Password reset / logout-all** — unchanged and globally authoritative. Recovery additionally
  revokes every trusted device and clears the replacement cooldown, so a Student who has just proven
  their mailbox is not made to wait 24 hours.
- **Session renewal / credential rotation** — untouched; the device binding lives on the family, not
  on a generation.
- **Cooldown as a lockout** — it is not one. Admin can revoke a device, revoke all devices, or reset
  the cooldown; recovery clears it; and only the Student's *own* removal or replacement starts it.
  An attacker holding only a live session cannot silently replace devices: replacement requires a
  fresh emailed code proving the mailbox.

## Known limitations

1. The presigned-segment residual above.
2. A copied device credential impersonates that device until it is removed.
3. Same-device multi-tab is resolved by **replacement**, not refusal: the newer playback wins and the
   older player stops at its next heartbeat (≤ one interval). Its already-issued segment URLs remain
   valid, per the residual.
4. The heartbeat is cooperative. A modified client can decline to stop; it still cannot start a
   second concurrent playback.
5. Device labels come from the User-Agent and are display-only. A browser reporting an unknown
   User-Agent shows "Unknown browser on Unknown platform".
6. In the combined release, Device Security owns migration `0037_student_trusted_devices`, following
   `0036_bundles_and_offers` and the current production schema 35. The renumber is integration-only;
   neither feature depends on the other.

## Verification-fixture isolation

Device-security browser journeys use a dedicated 50-Student pool. Its invitation timestamps are
older than the Admin queue window, and the established rotating pool remains exactly 35 slots / 350
Students. Device tests therefore cannot shift or crowd out the T8A entitlement fixtures. Generic
learning tests may establish a trusted Student device through the test-only seeder; the dedicated
device-security journey still drives the real Mailpit-delivered OTP through the product UI.
