#!/usr/bin/env bash
#
# agy-review.sh — dispatch an independent read-only review to the Antigravity CLI (`agy`).
#
# Usage:
#   scripts/agy-review.sh <base>..<head>
#   scripts/agy-review.sh <commit>          # reviews <commit>^..<commit>
#
# Which agent holds the builder seat and which holds the reviewer seat is decided per slice by a
# recorded decision, never by this script; the only invariant it enforces is that the reviewer's
# workspace is not the builder's. The agy-delegate skill is built for delegating
# *implementation* and offers no CLI-enforced read-only mode, so read-only is enforced structurally
# here instead of by trusting a flag or a prompt instruction:
#
#   * agy is pointed at a detached git worktree checked out at the exact reviewed commit, under the
#     scratch directory. The real repository — including the user-owned untracked spreadsheet — is
#     never in its workspace.
#   * That worktree is mounted read-only for the reviewer's process tree, and a writable scratch
#     directory is supplied outside it. A stray write fails with EROFS instead of tainting the run.
#     This is required, not best-effort: if read-only containment cannot be established and proven,
#     the run ends UNAVAILABLE before the model is invoked rather than degrading to something weaker.
#   * That worktree starts clean, so the relay's `touchedFiles` becomes a meaningful assertion.
#     Asserted against the main repo it would be permanently non-empty and prove nothing. It is
#     retained as defence in depth behind the read-only mount, not replaced by it.
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
#             verdict, or containment could not be established) · 6 INCONCLUSIVE (the live repo
#             changed during the run)
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
SCRATCH="$SCRATCH_BASE/gradex-review-scratch-$RUN_ID"
ARTIFACTS="$ARTIFACTS_ROOT/$RUN_ID"
mkdir -p "$ARTIFACTS" "$SCRATCH"

AGY_SETTINGS="${AGY_SETTINGS:-$HOME/.gemini/antigravity-cli/settings.json}"
SETTINGS_BACKUP="$ARTIFACTS/settings.json.orig"

cleanup() {
  # Restore the developer's agy settings first: leaving a scratch path trusted would outlive the run.
  if [ -f "$SETTINGS_BACKUP" ]; then
    cp -f "$SETTINGS_BACKUP" "$AGY_SETTINGS" && rm -f "$SETTINGS_BACKUP"
  fi
  # Undo the fallback permission lock before git is asked to delete the worktree, or the removal
  # fails and leaves a read-only checkout behind.
  if [ -n "${PERM_LOCKED:-}" ]; then chmod -R u+w "$WORKTREE" >/dev/null 2>&1 || true; fi
  git -C "$REPO_ROOT" worktree remove --force "$WORKTREE" >/dev/null 2>&1 || true
  git -C "$REPO_ROOT" worktree prune >/dev/null 2>&1 || true
  # The reviewer's scratch is disposable by construction; nothing in it is evidence.
  rm -rf "$SCRATCH"
}
trap cleanup EXIT

printf 'agy-review: range   %s\n' "$RANGE"
printf 'agy-review: model   %s\n' "$MODEL"
printf 'agy-review: tree    %s\n' "$WORKTREE"
printf 'agy-review: scratch %s\n' "$SCRATCH"
printf 'agy-review: output  %s\n' "$ARTIFACTS"

git -C "$REPO_ROOT" worktree add --detach "$WORKTREE" "$HEAD" >/dev/null

# --- render the fixed brief ----------------------------------------------------------------------
BRIEF="$ARTIFACTS/brief.txt"
awk '/<!-- BRIEF:BEGIN -->/{f=1;next} /<!-- BRIEF:END -->/{f=0} f' "$TEMPLATE" \
  | sed -e "s|{{RANGE}}|$RANGE|g" -e "s|{{BASE}}|$BASE|g" -e "s|{{HEAD}}|$HEAD|g" \
        -e "s|{{SCRATCH}}|$SCRATCH|g" \
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

# --- contain the reviewer: read-only checkout, writable scratch elsewhere -------------------------
# Detection after the fact is not containment. Two independent attempts on 2026-08-10 wrote
# REVIEW.md, patch.diff and scratch/ into their disposable worktree; both were correctly discarded
# as TAINTED, which costs an entire review run each time. The brief already forbade writes, so
# instruction alone is not enough. The reviewer now gets a real writable scratch directory outside
# the checkout, and the checkout is mounted read-only so a stray write fails at the OS boundary
# (EROFS) rather than silently tainting the run.
#
# The git index is not inside the worktree: a linked worktree's .git is a file pointing at the
# repository's common git dir, which is left writable. That is why git status/diff/log/show/grep
# still work against a read-only tree — the one thing a naive `chmod -R a-w` would break.
#
# The live repository is bound read-only too (its git dir and this run's artifacts directory
# excepted), so the reviewer cannot reach around the checkout into the working repo.
export AGY_SCRATCH="$SCRATCH"
export TMPDIR="$SCRATCH" TMP="$SCRATCH" TEMP="$SCRATCH"

GIT_COMMON_DIR="$(git -C "$REPO_ROOT" rev-parse --path-format=absolute --git-common-dir)"

#
# A real review FAILS CLOSED. If bwrap is missing, cannot start, or does not actually produce a
# read-only checkout, the run ends UNAVAILABLE *before the model is invoked*. It does not degrade to
# a weaker mechanism, because a verdict produced under weaker containment is a verdict nobody can
# rely on — and an unusable harness must never be able to look like a completed review.
#
# The permission-only fallback below is reachable only from the AGY_REVIEW_SELFTEST harness path,
# which produces no report and therefore cannot yield a verdict at all.
CONTAIN=()
PERM_LOCKED=""
CONTAINED=""

if command -v bwrap >/dev/null 2>&1 && bwrap --dev-bind / / true >/dev/null 2>&1; then
  CONTAIN=(bwrap
    --dev-bind / /
    --ro-bind "$REPO_ROOT" "$REPO_ROOT"
    --bind "$GIT_COMMON_DIR" "$GIT_COMMON_DIR"
    --bind "$ARTIFACTS" "$ARTIFACTS"
    --ro-bind "$WORKTREE" "$WORKTREE"
    --bind "$SCRATCH" "$SCRATCH"
    --die-with-parent
    --chdir "$WORKTREE")

  # Assert the boundary rather than assume it: bwrap can start and still leave the checkout
  # writable if a bind was ignored. The probe must fail to write and must succeed to read.
  if "${CONTAIN[@]}" bash -c '
        [ -e .git ] || exit 3
        touch .agy-containment-probe 2>/dev/null && exit 4
        [ -w "$AGY_SCRATCH" ] || exit 5
        exit 0
      ' >/dev/null 2>&1; then
    CONTAINED=bwrap
    printf 'agy-review: contained via bwrap — checkout read-only, writes land in the scratch dir\n'
  else
    printf 'agy-review: containment probe failed — the checkout is not verifiably read-only\n' >&2
  fi
else
  printf 'agy-review: bwrap is unavailable or cannot start on this host\n' >&2
fi

if [ -z "$CONTAINED" ]; then
  if [ -z "${AGY_REVIEW_SELFTEST:-}" ]; then
    printf 'agy-review: UNAVAILABLE — read-only containment could not be established, so no\n' >&2
    printf 'agy-review: review was dispatched and no model was invoked. This is not an approval,\n' >&2
    printf 'agy-review: and it is not a reason to relax containment: install bubblewrap (bwrap)\n' >&2
    printf 'agy-review: and re-run. Artifacts: %s\n' "$ARTIFACTS" >&2
    exit 5
  fi
  # Harness self-test only, and only ever reached with AGY_REVIEW_SELFTEST set. Weaker in two ways:
  # the same user can chmod it back, and it protects only the checkout — the live repository keeps
  # its normal permissions. No verdict can come out of this path: the self-test seam writes no
  # report, so the run still ends UNAVAILABLE.
  chmod -R a-w "$WORKTREE" || die "could not make the review worktree read-only" 3
  PERM_LOCKED=1
  printf 'agy-review: self-test only — permission-based fallback containment (no model is called)\n' >&2
fi

# --- dispatch ------------------------------------------------------------------------------------
# --dangerously-skip-permissions is required because agy's headless mode auto-denies every tool and
# returns an empty report. Developer-authorised for review runs; see the header. The grant applies to
# a disposable checkout, and the assertions below are what actually hold the line.
RESULT="$ARTIFACTS/result.json"
FINAL="$ARTIFACTS/final.txt"

RELAY_STATUS=0
if [ -n "${AGY_REVIEW_SELFTEST:-}" ]; then
  # Self-test seam for scripts/review-containment-check.sh: run an arbitrary command under exactly
  # the containment a reviewer would get, so the boundary can be proven without spending a review.
  # It cannot manufacture an approval — no final.txt is produced, so the run ends UNAVAILABLE.
  export AGY_REVIEW_BASE="$BASE" AGY_REVIEW_HEAD="$HEAD"
  export AGY_REVIEW_WORKTREE="$WORKTREE" AGY_REVIEW_REPO_ROOT="$REPO_ROOT"
  # cd first: this mirrors the --cd the relay hands agy, and it is the only thing that pins the
  # probe's cwd on the fallback path, where there is no bwrap --chdir.
  ( cd "$WORKTREE" && ${CONTAIN[@]+"${CONTAIN[@]}"} bash -c "$AGY_REVIEW_SELFTEST" ) || RELAY_STATUS=$?
  # Stand in for the relay's own measurement so the taint check below is exercised unchanged.
  git -C "$WORKTREE" status --porcelain \
    | awk 'BEGIN { printf "{\"status\":\"selftest\",\"touchedFiles\":[" }
           { gsub(/\\/, "\\\\"); gsub(/"/, "\\\""); printf "%s\"%s\"", (NR > 1 ? "," : ""), $0 }
           END { print "]}" }' > "$RESULT"
else
  ${CONTAIN[@]+"${CONTAIN[@]}"} node "$RELAY" \
    --brief "$BRIEF" \
    --cd "$WORKTREE" \
    --model "$MODEL" \
    --print-timeout "$PRINT_TIMEOUT" \
    --dangerously-skip-permissions \
    --out-dir "$ARTIFACTS" || RELAY_STATUS=$?
fi

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
