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
  "internal/video/playback.go" # HMAC signing boundary for playback tokens
  "internal/config/config.go"  # placeholder validation, inside the boundary itself
  # Two calls, both reviewed: the password plaintext goes to validation and
  # Argon2id hashing behind hashNewPassword, and the resulting encoded hash goes
  # to the database driver. Check 4 below enforces that the first of these is
  # the only password-plaintext exposure in the codebase.
  "internal/identity/bootstrap.go"
)

# The one production site permitted to read a password plaintext, and the
# marker comment that must sit immediately above it.
PASSWORD_BOUNDARY_FILE="internal/identity/bootstrap.go"
PASSWORD_BOUNDARY_MARKER="gradex:plaintext-boundary"

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
# Bootstrap password plaintext is exposed at exactly one reviewed production
# boundary: the Identity password-hashing operation.
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
  note "  The bootstrap password plaintext is exposed at exactly one reviewed"
  note "  production boundary: the Identity password-hashing operation. A second"
  note "  marker means a second plaintext read; remove it and route the value"
  note "  through hashNewPassword instead."
  fail=1
else
  marker_file="${markers[0]%%:*}"
  marker_line="${markers[0]#*:}"
  marker_line="${marker_line%%:*}"

  if [ "$marker_file" != "$PASSWORD_BOUNDARY_FILE" ]; then
    note "expose-guard: password-plaintext boundary moved to $marker_file"
    note "  It must stay in $PASSWORD_BOUNDARY_FILE, where it is reviewed."
    fail=1
  fi

  # The marker must actually mark an exposure. A marker that has drifted away
  # from its Expose() call documents a boundary that is no longer there.
  next_line=$(cd "$BACKEND" && sed -n "$((marker_line + 1))p" "$marker_file")
  if ! printf '%s' "$next_line" | grep -q '\.Expose()'; then
    note "expose-guard: the password-plaintext marker does not mark an Expose() call"
    note "  $marker_file:$((marker_line + 1)): $next_line"
    fail=1
  fi
fi

# A password Expose() anywhere outside the boundary file, including in an
# otherwise-allowlisted entrypoint.
if hits=$(cd "$BACKEND" && grep -rn -i '[Pp]assword[A-Za-z]*\.Expose()' --include='*.go' . 2>/dev/null \
    | grep -v '_test\.go:' \
    | sed 's|^\./||' \
    | grep -v "^$PASSWORD_BOUNDARY_FILE:"); then
  note "expose-guard: password plaintext exposed outside the reviewed boundary:"
  note "$hits"
  note "  Resolve the secret, keep it as config.Secret, and let Identity expose"
  note "  it once at the hashing boundary."
  fail=1
fi

if [ "$fail" -eq 0 ]; then
  echo "expose-guard: ok (${#call_sites[@]} approved call sites, 1 password-plaintext boundary)"
fi
exit "$fail"
