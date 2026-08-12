# T035a exposure-guard closeout

Date: 2026-08-12

Starting revision: `e3051409b2488c08cfc466fac9381133d5e3b7c4`

Authority: [D-087](../../../DECISIONS.md#d-087--t035a-may-cross-one-bounded-database-driver-secret-boundary-for-failure-evidence)

## Classification and boundary

`backend/cmd/e2e-media-diagnostic/main.go` resolves the run-owned media E2E `DATABASE_URL` as a
`config.Secret` and calls `Expose()` exactly once to pass the DSN directly to `db.Connect`, whose pgx
driver API requires a string. The failure-only diagnostic needs the authoritative run-owned
PostgreSQL state before normal teardown. No existing connector accepts the secret wrapper, so moving
the call would relocate rather than remove the plaintext boundary.

Classification: **`B — NECESSARY_BOUNDED_EXPOSURE`**.

The DSN originates in the diagnostic subprocess environment and travels only to the existing pgx
pool constructor. It is not placed in command arguments or filenames, forwarded to another
subprocess, logged, or added to the diagnostic input/output models. Connection failures emit only a
fixed stage label; error details are discarded. The exposure guard now documents and permits this
one exact file/boundary.

## Sanitization evidence

The existing T035a regression test now writes the actual retained JSON artifact after injecting
representative database and Redis passwords, Resend/provider and object-storage credentials,
session/CSRF/limiter secrets, upload and playback credentials, presigned query data, private object
keys, encrypted-payload plaintext, and invitation/reset/verification tokens into the only candidate
runtime/log inputs. None survives the safe runtime or bounded-log projections. The test also proves
the safe media route and worker operation remain available for correlation and that the artifact is
written with mode `0600`.

Successful media-authoring runs still invoke no collector. The diagnostic executable, collector,
production media/scanner/storage/authentication/authorization behavior, and retained evidence schema
are unchanged.

## Verification

| Command | Result |
|---|---|
| `cd backend && go test ./cmd/e2e-media-diagnostic ./internal/media/e2ediagnostic` | PASS |
| `cd backend && go test ./...` | PASS |
| `cd backend && go vet ./...` | PASS |
| `scripts/expose-guard.sh` | PASS (`15 approved call sites`, one pinned password boundary, two pinned password reads) |
| `scripts/docs-guard.sh` | PASS (`197 Markdown files`) |
| `git diff --check` | PASS |

No media-authoring E2E rerun was required: neither the diagnostic executable nor the success/failure
invocation path changed; the correction is the guard decision and its direct sanitizer regression.

## Review boundary

D-086's independent approval remains attached only to the frozen software head
`2c43b90fcf7a5c5913f42412fad5369911f781aa`. Current policy does not require a new independent
software review for this Product Owner-authorized documentation/test/security-guard correction,
which changes no production behavior. D-087 supplies the human-reviewed security decision required
by the exposure guard; this closeout does not represent the later commits as part of the historical
53-commit independent review.
