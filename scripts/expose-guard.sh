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
  "cmd/bootstrap-admin/main.go" # database connection for the one-off bootstrap operation
  "internal/video/playback.go" # HMAC signing boundary for playback tokens
  "internal/config/config.go"  # placeholder validation, inside the boundary itself
  # Argon2id hashing boundary. Two calls: the bootstrap plaintext goes to the
  # hasher, and the resulting encoded hash goes to the database driver. Neither
  # value travels further — not into a log, an error message, or a return
  # value. This is the only place in the codebase that reads a password
  # plaintext, which is why it earns an allowlist entry rather than a broader
  # package-level exemption.
  "internal/identity/bootstrap.go"
)

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

if [ "$fail" -eq 0 ]; then
  echo "expose-guard: ok (${#call_sites[@]} approved call sites)"
fi
exit "$fail"
