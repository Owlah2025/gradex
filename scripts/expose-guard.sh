#!/usr/bin/env bash
#
# expose-guard.sh — enforce where config.Secret.Expose() may be called.
#
# WHY
#   Secret redacts on every formatting and encoding path, so a secret cannot
#   leak by accident. Expose() is the one deliberate way to get the plaintext
#   back. That makes the set of call sites the actual security boundary: as
#   long as it stays small and reviewed, "secrets do not reach logs" is a
#   property of the code rather than a habit.
#
# WHAT IT CHECKS
#   1. Every non-test Expose() call site is on the allowlist below.
#   2. Expose() never appears in packages whose job is to emit data outward —
#      HTTP, logging, Problem Details, telemetry, serialization — regardless of
#      the allowlist.
#   3. A Secret is never handed to a formatting or encoding verb that would
#      bypass its redaction (%!s(MISSING)tring conversions and the like).
#
# CHANGING THE ALLOWLIST
#   Adding an entry is a security decision and needs human review. A reviewer
#   should be able to answer: does this call site hand the plaintext to a
#   driver, provider client, or signer, and does it hand it no further?
#
# USAGE
#   scripts/expose-guard.sh [repo-root]     # defaults to the repository root

set -euo pipefail

ROOT="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
BACKEND="$ROOT/backend"

# Approved production call sites, as "path:reason". Each hands the plaintext
# directly to something that must have it, and hands it no further.
ALLOWLIST=(
  "cmd/api/main.go"          # database, storage client construction
  "cmd/worker/main.go"       # database, storage client construction
  "cmd/migrate/main.go"      # database connection for schema migration
  # DSN only, for pgx.Connect, which cannot accept the wrapper. The bootstrap
  # password is NOT exposed here: it is resolved, checked with IsEmpty(), and
  # passed to Identity as a Secret. Adding a password Expose() to this file
  # would duplicate the plaintext boundary and must be rejected in review.
  "cmd/bootstrap-admin/main.go"
  "internal/config/config.go"  # placeholder validation, inside the boundary itself
  # Redis username/password cross directly into go-redis/asynq options. The
  # dedicated boundary check below pins the exact plaintext reads.
  "internal/queue/connection.go"
  # Resend requires the API key in the HTTPS Authorization header. The adapter
  # reads it only while constructing that request; response/error paths expose
  # neither the header nor raw provider content.
  "internal/email/resend.go"
  # The encoded Argon2id hash goes to the database driver. No password plaintext
  # is read here — that happens only in credential.go, which check 4 enforces.
  "internal/identity/bootstrap.go"
  # The encoded replacement Argon2id hash goes to PostgreSQL when password,
  # restriction state, Audit, and session rotation commit atomically. No
  # password plaintext is read here.
  "internal/identity/password_change.go"
  # Argon2id credential hash and fresh action bearer cross directly into
  # PostgreSQL and authenticated outbox encryption during atomic admission.
  "internal/identity/admission.go"
  # The fresh password-reset bearer crosses directly into authenticated outbox
  # encryption inside the atomic reset-request transaction, and no further. It
  # is never logged, never returned in a response, and never stored: only its
  # SHA-256 digest reaches identity_action_secrets. Same boundary and same
  # reasoning as admission.go above, for the PASSWORD_RESET purpose.
  "internal/identity/recovery.go"
  # The password-plaintext boundary itself. See check 4.
  "internal/identity/credential.go"
  # The fresh staff-invitation bearer crosses directly into response and
  # outbox encryption, and no further. Same reasoning as recovery.go above.
  "internal/identity/invitation.go"
  # Opaque session and CSRF plaintext cross only into the hardened cookie and
  # no-store JSON body after their authoritative transaction has committed.
  "internal/auth/session_response.go"
)

# The one production file permitted to read a password plaintext, the marker
# that must name it, and the exact number of Expose() calls it may contain.
#
# The count is pinned rather than merely bounded. Link 4 legitimately added a
# second plaintext read — verifying the current password for a voluntary change
# — so the count moved from 1 to 2 as a reviewed decision. Pinning it means the
# next addition also has to be a reviewed decision instead of arriving silently.
PASSWORD_BOUNDARY_FILE="internal/identity/credential.go"
PASSWORD_BOUNDARY_MARKER="gradex:plaintext-boundary"
PASSWORD_BOUNDARY_EXPOSURES=2

REDIS_PASSWORD_BOUNDARY_FILE="internal/queue/connection.go"
REDIS_PASSWORD_BOUNDARY_MARKER="gradex:redis-password-plaintext-boundary"
REDIS_PASSWORD_BOUNDARY_EXPOSURES=2

# Packages that exist to send data outward. Expose() here is a defect even if
# someone adds the file to the allowlist, so this check runs independently.
FORBIDDEN_DIRS=(
  "internal/httpapi"
  "internal/logging"
  "internal/problem"
  "internal/requestid"
)

fail=0

note() { printf '%s\n' "$*" >&2; }

# --- 1. call sites outside the allowlist ------------------------------------
# Test files are excluded: they exercise the boundary rather than cross it.
mapfile -t call_sites < <(
  cd "$BACKEND" && grep -rln '\.Expose()' --include='*.go' . 2>/dev/null \
    | grep -v '_test\.go$' \
    | sed 's|^\./||' \
    | sort
)

for site in "${call_sites[@]:-}"; do
  [ -z "$site" ] && continue
  approved=0
  for allowed in "${ALLOWLIST[@]}"; do
    if [ "$site" = "${allowed%% *}" ]; then approved=1; break; fi
  done
  if [ "$approved" -eq 0 ]; then
    note "expose-guard: unapproved Secret.Expose() call site: backend/$site"
    note "  If this hands the plaintext to a driver, provider client, or signer"
    note "  and no further, add it to ALLOWLIST in scripts/expose-guard.sh with"
    note "  a reason. That edit requires human review."
    fail=1
  fi
done

# --- 2. forbidden packages --------------------------------------------------
for dir in "${FORBIDDEN_DIRS[@]}"; do
  [ -d "$BACKEND/$dir" ] || continue
  if hits=$(cd "$BACKEND" && grep -rn '\.Expose()' --include='*.go' "$dir" 2>/dev/null | grep -v '_test\.go:'); then
    note "expose-guard: Secret.Expose() in an outward-facing package:"
    note "$hits"
    note "  These packages emit data to clients or logs. A secret must be"
    note "  resolved before it reaches them, never inside them."
    fail=1
  fi
done

# --- 3. redaction bypasses --------------------------------------------------
# string(secret.Expose()) and similar re-wrap the plaintext as a plain string,
# which then formats without redaction.
if hits=$(cd "$BACKEND" && grep -rn 'string(.*\.Expose())' --include='*.go' . 2>/dev/null | grep -v '_test\.go:'); then
  note "expose-guard: plaintext re-wrapped as a bare string:"
  note "$hits"
  fail=1
fi

# --- 4. the password-plaintext boundary -------------------------------------
# Account password plaintext is exposed at exactly one reviewed production
# boundary: the Identity credential operation in credential.go, which validates
# and hashes a new password and verifies a current one.
#
# The other checks bound which *files* may call Expose(). This one bounds the
# password specifically, because a second plaintext read is a duplicated
# handling path even inside an already-approved file — and duplicated handling
# is how a plaintext ends up in an error string, an Audit row, or a struct
# field that later gets logged.
mapfile -t markers < <(
  cd "$BACKEND" && grep -rn "$PASSWORD_BOUNDARY_MARKER" --include='*.go' . 2>/dev/null \
    | grep -v '_test\.go:' \
    | sed 's|^\./||' \
    | sort
)

if [ "${#markers[@]}" -ne 1 ]; then
  note "expose-guard: expected exactly 1 password-plaintext boundary marker, found ${#markers[@]}"
  for m in "${markers[@]:-}"; do note "  $m"; done
  note "  Password plaintext is read at exactly one reviewed boundary. A second"
  note "  marker means a second boundary; remove it and route the value through"
  note "  prepareCredential instead."
  fail=1
else
  marker_file="${markers[0]%%:*}"
  marker_line="${markers[0]#*:}"
  marker_line="${marker_line%%:*}"

  if [ "$marker_file" != "$PASSWORD_BOUNDARY_FILE" ]; then
    note "expose-guard: password-plaintext boundary moved to $marker_file"
    note "  It must stay in $PASSWORD_BOUNDARY_FILE, where it is reviewed."
    fail=1
  else
    # Every Expose() in the boundary file must sit after the marker, inside the
    # function it introduces. A read placed above the marker would be outside
    # the reviewed boundary while still living in the reviewed file.
    exposures=$(cd "$BACKEND" && grep -c '\.Expose()' "$marker_file" || true)
    if [ "$exposures" -ne "$PASSWORD_BOUNDARY_EXPOSURES" ]; then
      note "expose-guard: $marker_file has $exposures Expose() calls, expected $PASSWORD_BOUNDARY_EXPOSURES"
      note "  Each password-plaintext read is a reviewed decision. If this one is"
      note "  intended, update PASSWORD_BOUNDARY_EXPOSURES and say why in review."
      fail=1
    fi

    early=$(cd "$BACKEND" && grep -n '\.Expose()' "$marker_file" | awk -F: -v m="$marker_line" '$1 < m')
    if [ -n "$early" ]; then
      note "expose-guard: password plaintext is read above the boundary marker:"
      note "$early"
      fail=1
    fi
  fi
fi

# Redis credentials have a separate driver-only boundary. Both the username and
# password reads are pinned so another plaintext handling path cannot arrive
# silently inside an otherwise-approved adapter.
mapfile -t redis_markers < <(
  cd "$BACKEND" && grep -rn "$REDIS_PASSWORD_BOUNDARY_MARKER" --include='*.go' . 2>/dev/null \
    | grep -v '_test\.go:' | sed 's|^\./||' | sort
)

if [ "${#redis_markers[@]}" -ne 1 ]; then
  note "expose-guard: expected exactly 1 Redis credential boundary marker, found ${#redis_markers[@]}"
  fail=1
else
  redis_marker_file="${redis_markers[0]%%:*}"
  redis_marker_line="${redis_markers[0]#*:}"
  redis_marker_line="${redis_marker_line%%:*}"
  if [ "$redis_marker_file" != "$REDIS_PASSWORD_BOUNDARY_FILE" ]; then
    note "expose-guard: Redis credential boundary moved to $redis_marker_file"
    fail=1
  else
    redis_exposures=$(cd "$BACKEND" && grep -c '\.Expose()' "$redis_marker_file" || true)
    if [ "$redis_exposures" -ne "$REDIS_PASSWORD_BOUNDARY_EXPOSURES" ]; then
      note "expose-guard: $redis_marker_file has $redis_exposures Expose() calls, expected $REDIS_PASSWORD_BOUNDARY_EXPOSURES"
      fail=1
    fi
    redis_early=$(cd "$BACKEND" && grep -n '\.Expose()' "$redis_marker_file" | awk -F: -v m="$redis_marker_line" '$1 < m')
    if [ -n "$redis_early" ]; then
      note "expose-guard: Redis credential plaintext is read above its boundary marker:"
      note "$redis_early"
      fail=1
    fi
  fi
fi

# A password Expose() anywhere outside the two reviewed boundary files,
# including in an otherwise-allowlisted entrypoint.
if hits=$(cd "$BACKEND" && grep -rn -i '[Pp]assword[A-Za-z]*\.Expose()' --include='*.go' . 2>/dev/null \
  | grep -v '_test\.go:' \
  | sed 's|^\./||' \
  | grep -v "^$PASSWORD_BOUNDARY_FILE:" \
  | grep -v "^$REDIS_PASSWORD_BOUNDARY_FILE:"); then
  note "expose-guard: password plaintext exposed outside the reviewed boundary:"
  note "$hits"
  note "  Resolve the secret, keep it as config.Secret, and let Identity expose"
  note "  it once at the hashing boundary."
  fail=1
fi

# --- 5. retired D7 legacy media authority ----------------------------------
# D7 owns the only production path that turns committed media outbox rows into
# Asynq work. These checks are deliberately semantic rather than filename-only:
# a direct enqueue or task creation under a new filename is still a second,
# unsafe media authority. The live production-router test remains the primary
# composition proof; this guard prevents a simple source-level resurrection
# from slipping through before that test is reached.
LEGACY_MEDIA_ROUTE_PATTERN='"(/api/v1)?/lessons/:lessonID/(video|progress)|"(/api/v1)?/videos/:videoID/manifest'
if hits=$(cd "$BACKEND" && grep -rnE "$LEGACY_MEDIA_ROUTE_PATTERN" --include='*.go' internal cmd 2>/dev/null \
  | grep -v '_test\.go:' || true); then
  if [ -n "$hits" ]; then
    note "expose-guard: retired legacy media HTTP route source was reintroduced:"
    note "$hits"
    fail=1
  fi
fi

mapfile -t retired_video_files < <(
  find "$BACKEND/internal/video" -maxdepth 1 -type f -name '*.go' ! -name '*_test.go' -printf '%P\n' 2>/dev/null | sort
)
if [ "${#retired_video_files[@]}" -ne 0 ]; then
  note "expose-guard: retired production internal/video implementation returned:"
  for file in "${retired_video_files[@]}"; do note "  backend/internal/video/$file"; done
  fail=1
fi
if hits=$(cd "$BACKEND" && grep -rnlE '^package[[:space:]]+video$' --include='*.go' internal cmd 2>/dev/null \
  | grep -v '_test\.go$' || true); then
  if [ -n "$hits" ]; then
    note "expose-guard: retired production video package returned:"
    note "$hits"
    fail=1
  fi
fi

APPROVED_MEDIA_DISPATCHER='internal/media/dispatcher.go'
mapfile -t media_task_sites < <(
  cd "$BACKEND" && grep -rlnE 'asynq\.NewTask[[:space:]]*\(' --include='*.go' internal cmd 2>/dev/null \
    | grep -v '_test\.go$' | sed 's|^\./||' | sort
)
for site in "${media_task_sites[@]:-}"; do
  [ -z "$site" ] && continue
  if [ "$site" != "$APPROVED_MEDIA_DISPATCHER" ]; then
    note "expose-guard: direct Asynq task construction outside committed media dispatcher: backend/$site"
    fail=1
  fi
done

mapfile -t enqueue_sites < <(
  cd "$BACKEND" && grep -rlnE '\.(Enqueue|EnqueueContext)[[:space:]]*\(' --include='*.go' internal cmd 2>/dev/null \
    | grep -v '_test\.go$' | sed 's|^\./||' | sort
)
for site in "${enqueue_sites[@]:-}"; do
  [ -z "$site" ] && continue
  if [ "$site" != "$APPROVED_MEDIA_DISPATCHER" ]; then
    note "expose-guard: direct queue enqueue outside committed media dispatcher: backend/$site"
    fail=1
  fi
done

if [ "$fail" -eq 0 ]; then
  echo "expose-guard: ok (${#call_sites[@]} approved call sites, 1 password-plaintext boundary, ${PASSWORD_BOUNDARY_EXPOSURES} reviewed plaintext reads)"
fi
exit "$fail"
