#!/usr/bin/env bash
#
# agy-review.sh — dispatch an independent read-only review to the Antigravity CLI (`agy`).
#
# Usage:
#   scripts/agy-review.sh <base>..<head>
#   scripts/agy-review.sh <commit>          # reviews <commit>^..<commit>
#
# Under D-033 Codex is the builder and Claude is the default independent reviewer. When Claude is
# unavailable, agy remains the D-032 fallback. The agy-delegate skill is built for delegating
# *implementation* and offers no CLI-enforced read-only mode, so read-only is enforced structurally
# here instead of by trusting a flag or a prompt instruction:
#
#   * agy is pointed at a detached git worktree checked out at the exact reviewed commit, under the
#     scratch directory. The real repository — including the user-owned untracked spreadsheet — is
#     never in its workspace.
#   * That worktree starts clean, so the relay's `touchedFiles` becomes a meaningful assertion.
#     Asserted against the main repo it would be permanently non-empty and prove nothing.
#   * The main repo's porcelain status is snapshotted before and after and must match exactly.
#
# PERMISSIONS. agy's headless mode cannot prompt, so it auto-denies every tool — including the reads
# a review is made of — and returns an empty report. The developer explicitly authorised
# --dangerously-skip-permissions for review runs on 2026-07-25, on the basis that the grant is
# scoped to a disposable checkout rather than to the working repository. Treat every review run as
# full access. The containment above, not the flag, is what makes it safe.
#
# TRUSTED WORKSPACE. agy resets its shell cwd into a directory from settings.trustedWorkspaces when
# the current one is untrusted. Left alone it would relocate into the live repo and review the dirty
# working tree instead of the frozen commit — with full permissions. So the review worktree is added
# to trustedWorkspaces for the duration of the run and the original settings file is restored on
# exit, including on failure.
#
# Any modification means the review is discarded as TAINTED, not corrected. A review whose report
# carries no parseable VERDICT line is reported as UNAVAILABLE, never as a pass — see
# docs/launch/daily/2026-07-27.md, where three non-interactive review invocations completed without a
# retrievable verdict and produced no usable evidence.
#
# Reasoning effort needs no relay change: the model label already encodes it.
#
# Exit codes: 0 review completed and parseable · 2 usage error · 3 relay/agy failure
#             4 TAINTED (the reviewer modified its workspace) · 5 UNAVAILABLE (no retrievable
#             verdict) · 6 INCONCLUSIVE (the live repo changed during the run)
#
# 4 and 6 are different failures. TAINTED means the reviewer definitively broke read-only and the
# review is void. INCONCLUSIVE means the reviewer's own workspace was clean but the live repo moved
# underneath the run, so the drift is unexplained — usually the builder editing in parallel. Set
# AGY_REVIEW_ALLOW_CONCURRENT_EDITS=1 to acknowledge that and downgrade it to a warning; the reviewed
# commit is frozen in the worktree either way, so the verdict itself stays valid.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEMPLATE="$REPO_ROOT/docs/launch/review/REVIEW_BRIEF_TEMPLATE.md"
ARTIFACTS_ROOT="$REPO_ROOT/docs/launch/review/artifacts"

MODEL="${AGY_REVIEW_MODEL:-gemini-3.1-pro-high}"
PRINT_TIMEOUT="${AGY_REVIEW_TIMEOUT:-30m}"
SCRATCH_BASE="${AGY_REVIEW_SCRATCH:-${TMPDIR:-/tmp}}"

die() { printf 'agy-review: %s\n' "$*" >&2; exit "${2:-2}"; }

# --- locate the relay from the installed agy-delegate skill (never a vendored copy) -------------
RELAY=""
for candidate in \
  "${AGY_DELEGATE_RELAY:-}" \
  "$HOME/.claude/skills/agy-delegate/scripts/relay.mjs" \
  "$HOME/.agents/skills/agy-delegate/scripts/relay.mjs" \
  "$REPO_ROOT/.claude/skills/agy-delegate/scripts/relay.mjs"; do
  [ -n "$candidate" ] && [ -f "$candidate" ] && { RELAY="$candidate"; break; }
done
[ -n "$RELAY" ] || die "agy-delegate relay not found. Install the skill or set AGY_DELEGATE_RELAY."

command -v agy  >/dev/null 2>&1 || die "\`agy\` not on PATH. Install the Antigravity CLI and complete first-launch setup."
command -v node >/dev/null 2>&1 || die "\`node\` not on PATH (required by the relay)."
[ -f "$TEMPLATE" ] || die "review brief template missing: $TEMPLATE"

# --- resolve the exact reviewed range ------------------------------------------------------------
[ $# -eq 1 ] || die "usage: scripts/agy-review.sh <base>..<head> | <commit>"
ARG="$1"

if [[ "$ARG" == *".."* ]]; then
  BASE_REF="${ARG%%..*}"
  HEAD_REF="${ARG##*..}"
else
  BASE_REF="${ARG}^"
  HEAD_REF="$ARG"
fi

BASE="$(git -C "$REPO_ROOT" rev-parse --verify --quiet "${BASE_REF}^{commit}")" \
  || die "not a commit: $BASE_REF"
HEAD="$(git -C "$REPO_ROOT" rev-parse --verify --quiet "${HEAD_REF}^{commit}")" \
  || die "not a commit: $HEAD_REF"

[ "$BASE" != "$HEAD" ] || die "base and head are the same commit; there is nothing to review"

RANGE="$(git -C "$REPO_ROOT" rev-parse --short "$BASE")..$(git -C "$REPO_ROOT" rev-parse --short "$HEAD")"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
RUN_ID="${RANGE//../-}-$STAMP"

# --- snapshot the real repo so any escape is detectable ------------------------------------------
BEFORE="$(git -C "$REPO_ROOT" status --porcelain)"

WORKTREE="$SCRATCH_BASE/gradex-review-$RUN_ID"
ARTIFACTS="$ARTIFACTS_ROOT/$RUN_ID"
mkdir -p "$ARTIFACTS"

AGY_SETTINGS="${AGY_SETTINGS:-$HOME/.gemini/antigravity-cli/settings.json}"
SETTINGS_BACKUP="$ARTIFACTS/settings.json.orig"

cleanup() {
  # Restore the developer's agy settings first: leaving a scratch path trusted would outlive the run.
  if [ -f "$SETTINGS_BACKUP" ]; then
    cp -f "$SETTINGS_BACKUP" "$AGY_SETTINGS" && rm -f "$SETTINGS_BACKUP"
  fi
  git -C "$REPO_ROOT" worktree remove --force "$WORKTREE" >/dev/null 2>&1 || true
  git -C "$REPO_ROOT" worktree prune >/dev/null 2>&1 || true
}
trap cleanup EXIT

printf 'agy-review: range   %s\n' "$RANGE"
printf 'agy-review: model   %s\n' "$MODEL"
printf 'agy-review: tree    %s\n' "$WORKTREE"
printf 'agy-review: output  %s\n' "$ARTIFACTS"

git -C "$REPO_ROOT" worktree add --detach "$WORKTREE" "$HEAD" >/dev/null

# --- render the fixed brief ----------------------------------------------------------------------
BRIEF="$ARTIFACTS/brief.txt"
awk '/<!-- BRIEF:BEGIN -->/{f=1;next} /<!-- BRIEF:END -->/{f=0} f' "$TEMPLATE" \
  | sed -e "s|{{RANGE}}|$RANGE|g" -e "s|{{BASE}}|$BASE|g" -e "s|{{HEAD}}|$HEAD|g" \
  > "$BRIEF"

[ -s "$BRIEF" ] || die "rendered brief is empty; check the BRIEF:BEGIN/END markers in $TEMPLATE"

# --- trust the disposable worktree so agy does not relocate into the live repo --------------------
if [ -f "$AGY_SETTINGS" ]; then
  cp -f "$AGY_SETTINGS" "$SETTINGS_BACKUP"
  node -e '
    const fs = require("fs");
    const [file, tree] = process.argv.slice(1);
    const s = JSON.parse(fs.readFileSync(file, "utf8"));
    s.trustedWorkspaces = Array.from(new Set([...(s.trustedWorkspaces || []), tree]));
    fs.writeFileSync(file, JSON.stringify(s, null, 2) + "\n");
  ' "$AGY_SETTINGS" "$WORKTREE" || die "could not add the review worktree to agy trustedWorkspaces" 3
  printf 'agy-review: trusted %s for this run (settings restored on exit)\n' "$WORKTREE"
else
  printf 'agy-review: warning — %s not found; agy may relocate its shell out of the worktree\n' "$AGY_SETTINGS" >&2
fi

# --- dispatch ------------------------------------------------------------------------------------
# --dangerously-skip-permissions is required because agy's headless mode auto-denies every tool and
# returns an empty report. Developer-authorised for review runs; see the header. The grant applies to
# a disposable checkout, and the assertions below are what actually hold the line.
RELAY_STATUS=0
node "$RELAY" \
  --brief "$BRIEF" \
  --cd "$WORKTREE" \
  --model "$MODEL" \
  --print-timeout "$PRINT_TIMEOUT" \
  --dangerously-skip-permissions \
  --out-dir "$ARTIFACTS" || RELAY_STATUS=$?

RESULT="$ARTIFACTS/result.json"
FINAL="$ARTIFACTS/final.txt"

# --- assert read-only, before reading any verdict ------------------------------------------------
# Order matters: a tainted run is discarded whatever it concluded.
TAINTED=""

if [ -f "$RESULT" ]; then
  TOUCHED="$(node -e '
    const r = require(process.argv[1]);
    if (r.touchedFiles === null) { console.log("UNKNOWN"); }
    else if (r.touchedFiles.length === 0) { console.log("CLEAN"); }
    else { console.log("DIRTY:" + r.touchedFiles.join("; ")); }
  ' "$RESULT")"
else
  TOUCHED="NORESULT"
fi

case "$TOUCHED" in
  CLEAN)    printf 'agy-review: worktree clean — reviewer made no edits\n' ;;
  DIRTY:*)  TAINTED="reviewer modified the review worktree: ${TOUCHED#DIRTY:}" ;;
  UNKNOWN)  TAINTED="relay could not report worktree state (git unavailable); cannot prove read-only" ;;
  NORESULT) printf 'agy-review: no result.json — relay failed before it could run\n' ;;
esac

if [ -n "$TAINTED" ]; then
  printf 'agy-review: TAINTED — %s\n' "$TAINTED" >&2
  printf 'agy-review: this review is discarded, not corrected. Do not record it as evidence.\n' >&2
  exit 4
fi

# The live repo is not the reviewer's workspace, so drift here is a weaker signal than a dirty
# worktree — but an unexplained change is still unexplained, so it must be acknowledged, not ignored.
AFTER="$(git -C "$REPO_ROOT" status --porcelain)"
DRIFTED=""
if [ "$BEFORE" != "$AFTER" ]; then
  DRIFTED=1
  printf '%s\n' "$BEFORE" > "$ARTIFACTS/main-repo-status.before"
  printf '%s\n' "$AFTER"  > "$ARTIFACTS/main-repo-status.after"
  printf 'agy-review: live repo changed during the run:\n' >&2
  diff <(printf '%s\n' "$BEFORE") <(printf '%s\n' "$AFTER") | sed 's/^/  /' >&2 || true
fi

if [ "$RELAY_STATUS" -ne 0 ]; then
  printf 'agy-review: relay exited %s — review did not complete. Artifacts: %s\n' "$RELAY_STATUS" "$ARTIFACTS" >&2
  exit 3
fi

# --- assert the verdict is actually retrievable ---------------------------------------------------
if [ ! -s "$FINAL" ] || ! grep -qE '^VERDICT: (APPROVE|APPROVE WITH FINDINGS|REJECT)$' "$FINAL"; then
  printf 'agy-review: UNAVAILABLE — no parseable VERDICT line in %s\n' "$FINAL" >&2
  printf 'agy-review: record this as review UNAVAILABLE. It is not an approval.\n' >&2
  exit 5
fi

VERDICT="$(grep -E '^VERDICT: ' "$FINAL" | tail -1)"

if [ -n "$DRIFTED" ] && [ -z "${AGY_REVIEW_ALLOW_CONCURRENT_EDITS:-}" ]; then
  printf 'agy-review: INCONCLUSIVE — the reviewer workspace was clean, but the live repo changed\n' >&2
  printf 'agy-review: during the run and nothing declared that. The verdict below is against the\n' >&2
  printf 'agy-review: frozen commit and is readable, but do not record it until the drift is\n' >&2
  printf 'agy-review: explained. Re-run with AGY_REVIEW_ALLOW_CONCURRENT_EDITS=1 if it was you.\n' >&2
  printf 'agy-review: %s (not recordable)\n' "$VERDICT" >&2
  exit 6
fi

[ -n "$DRIFTED" ] && printf 'agy-review: note — live repo changed during the run; acknowledged by the builder\n' >&2

printf '\n===== independent review: %s =====\n' "$RANGE"
sed -n '/^FINDINGS/,$p' "$FINAL" || true
printf '\nagy-review: %s\n' "$VERDICT"
printf 'agy-review: reviewed %s with %s · artifacts %s\n' "$RANGE" "$MODEL" "$ARTIFACTS"
