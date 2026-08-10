#!/usr/bin/env bash
#
# review-containment-check.sh — prove that scripts/agy-review.sh contains its reviewer.
#
# Two independent review attempts on 2026-08-10 wrote REVIEW.md, patch.diff and scratch/ into their
# disposable review worktree and were discarded as TAINTED. The brief already forbade writes, so the
# fix was containment rather than more instruction: the checkout is mounted read-only and a writable
# scratch directory is supplied outside it. This check asserts that boundary end-to-end through the
# real dispatcher, using its AGY_REVIEW_SELFTEST seam so no review is spent and no model is called.
#
# It reviews nothing. It proves:
#   1. the reviewer's workspace is the frozen source tree
#   2. git status/diff/log/show/grep/ls-tree all still work there
#   3. creating a file in the review root fails
#   4. creating a directory in the review root fails
#   5. modifying a tracked file fails
#   6. writing to the supplied scratch directory succeeds
#   7. writing into the live repository fails
#   8. the post-run touched-files guard reports a clean worktree
#   9. the live repository is byte-for-byte unchanged and the temporaries are gone
#  10. a real review with bwrap unavailable exits UNAVAILABLE without invoking the model
#  11. the permission-only fallback is unreachable for a real review, so it cannot yield a verdict
#
# Usage: scripts/review-containment-check.sh [<base>..<head>]
# Defaults to HEAD~1..HEAD; any small range works, since nothing is actually reviewed.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RANGE_ARG="${1:-HEAD~1..HEAD}"

pass() { printf '  ok   %s\n' "$*"; }
fail() { printf '  FAIL %s\n' "$*" >&2; FAILURES=$((FAILURES + 1)); }
FAILURES=0

BEFORE="$(git -C "$REPO_ROOT" status --porcelain)"

# The probe runs inside the reviewer's containment, as the reviewer would. It prints one marker per
# assertion; nothing here may modify the repository, and every attempt to do so must fail.
PROBE='
set -u
printf "PROBE_PWD=%s\n" "$(pwd)"
[ "$(pwd)" = "$AGY_REVIEW_WORKTREE" ] && echo PROBE_CWD_OK

git rev-parse HEAD | grep -q "^$AGY_REVIEW_HEAD$" && echo PROBE_FROZEN_HEAD_OK

git status --short >/dev/null 2>&1                          && echo PROBE_GIT_STATUS_OK
git diff "$AGY_REVIEW_BASE".."$AGY_REVIEW_HEAD" --stat >/dev/null 2>&1 && echo PROBE_GIT_DIFF_OK
git log --oneline -5 >/dev/null 2>&1                        && echo PROBE_GIT_LOG_OK
git show --stat HEAD >/dev/null 2>&1                        && echo PROBE_GIT_SHOW_OK
git grep -l "" -- . >/dev/null 2>&1                         && echo PROBE_GIT_GREP_OK
git ls-tree HEAD >/dev/null 2>&1                            && echo PROBE_GIT_LS_TREE_OK

touch REVIEW.md 2>/dev/null && echo PROBE_TOUCH_SUCCEEDED || echo PROBE_TOUCH_BLOCKED
mkdir scratch    2>/dev/null && echo PROBE_MKDIR_SUCCEEDED || echo PROBE_MKDIR_BLOCKED

TRACKED="$(git ls-tree -r --name-only HEAD | head -1)"
printf "PROBE_TRACKED=%s\n" "$TRACKED"
printf "x\n" >> "$TRACKED" 2>/dev/null && echo PROBE_TRACKED_WRITE_SUCCEEDED || echo PROBE_TRACKED_WRITE_BLOCKED

touch "$AGY_REVIEW_REPO_ROOT/CONTAINMENT_ESCAPE" 2>/dev/null \
  && echo PROBE_LIVE_REPO_WRITE_SUCCEEDED || echo PROBE_LIVE_REPO_WRITE_BLOCKED

printf "diff\n" > "$AGY_SCRATCH/patch.diff" 2>/dev/null && echo PROBE_SCRATCH_WRITE_OK
[ -s "$AGY_SCRATCH/patch.diff" ] && echo PROBE_SCRATCH_READBACK_OK
[ "$TMPDIR" = "$AGY_SCRATCH" ] && echo PROBE_TMPDIR_OK
exit 0
'

printf 'review-containment-check: dispatching self-test over %s\n' "$RANGE_ARG"

OUT=""
STATUS=0
OUT="$(AGY_REVIEW_SELFTEST="$PROBE" "$REPO_ROOT/scripts/agy-review.sh" "$RANGE_ARG" 2>&1)" || STATUS=$?

printf '%s\n' "$OUT" | sed 's/^/  | /'
printf 'review-containment-check: dispatcher exit %s\n' "$STATUS"

expect_marker() {
  if printf '%s\n' "$OUT" | grep -qx "$1"; then pass "$2"; else fail "$2 (missing marker $1)"; fi
}
reject_marker() {
  if printf '%s\n' "$OUT" | grep -qx "$1"; then fail "$2 (saw $1)"; else pass "$2"; fi
}

echo
echo 'review-containment-check: assertions'

expect_marker PROBE_CWD_OK          'reviewer works in the disposable review worktree'
expect_marker PROBE_FROZEN_HEAD_OK  'that worktree is checked out at the reviewed head'

expect_marker PROBE_GIT_STATUS_OK   'git status works read-only'
expect_marker PROBE_GIT_DIFF_OK     'git diff <base>..<head> works read-only'
expect_marker PROBE_GIT_LOG_OK      'git log works read-only'
expect_marker PROBE_GIT_SHOW_OK     'git show works read-only'
expect_marker PROBE_GIT_GREP_OK     'git grep works read-only'
expect_marker PROBE_GIT_LS_TREE_OK  'git ls-tree works read-only'

expect_marker PROBE_TOUCH_BLOCKED         'creating REVIEW.md in the review root fails'
reject_marker PROBE_TOUCH_SUCCEEDED       'creating REVIEW.md in the review root is not possible'
expect_marker PROBE_MKDIR_BLOCKED         'creating scratch/ in the review root fails'
reject_marker PROBE_MKDIR_SUCCEEDED       'creating scratch/ in the review root is not possible'
expect_marker PROBE_TRACKED_WRITE_BLOCKED 'modifying a tracked source file fails'
reject_marker PROBE_TRACKED_WRITE_SUCCEEDED 'modifying a tracked source file is not possible'
expect_marker PROBE_LIVE_REPO_WRITE_BLOCKED 'writing into the live repository fails'
reject_marker PROBE_LIVE_REPO_WRITE_SUCCEEDED 'writing into the live repository is not possible'

expect_marker PROBE_SCRATCH_WRITE_OK    'writing $AGY_SCRATCH/patch.diff succeeds'
expect_marker PROBE_SCRATCH_READBACK_OK 'the scratch file is readable afterwards'
expect_marker PROBE_TMPDIR_OK           'TMPDIR points at the external scratch directory'

if printf '%s\n' "$OUT" | grep -q 'worktree clean — reviewer made no edits'; then
  pass 'post-run touched-files guard reports a clean worktree'
else
  fail 'post-run touched-files guard did not report a clean worktree'
fi

if printf '%s\n' "$OUT" | grep -q 'TAINTED'; then
  fail 'run was reported TAINTED'
else
  pass 'run was not TAINTED'
fi

# The self-test produces no report, so the dispatcher must end UNAVAILABLE (5): the seam cannot be
# used to manufacture an approval.
if [ "$STATUS" -eq 5 ]; then
  pass 'dispatcher exits 5 UNAVAILABLE (self-test produces no verdict)'
else
  fail "dispatcher exit was $STATUS, expected 5 UNAVAILABLE"
fi

AFTER="$(git -C "$REPO_ROOT" status --porcelain)"
if [ "$BEFORE" = "$AFTER" ]; then
  pass 'live repository is unchanged'
else
  fail 'live repository changed during the check'
  diff <(printf '%s\n' "$BEFORE") <(printf '%s\n' "$AFTER") | sed 's/^/    /' >&2 || true
fi

[ -e "$REPO_ROOT/CONTAINMENT_ESCAPE" ] && fail 'escape probe created a file in the live repository' \
  || pass 'no escape artifact in the live repository'

LEFTOVER="$(printf '%s\n' "$OUT" | sed -n 's/^agy-review: tree    //p')"
SCRATCH_DIR="$(printf '%s\n' "$OUT" | sed -n 's/^agy-review: scratch //p')"
[ -n "$LEFTOVER" ] && [ ! -e "$LEFTOVER" ] && pass 'review worktree removed on exit' \
  || fail "review worktree not cleaned up: $LEFTOVER"
[ -n "$SCRATCH_DIR" ] && [ ! -e "$SCRATCH_DIR" ] && pass 'scratch directory removed on exit' \
  || fail "scratch directory not cleaned up: $SCRATCH_DIR"

# --- fail-closed: no strong containment means no review, and no model call ------------------------
# The dispatcher must refuse rather than degrade. Proven by shadowing bwrap with a stub that always
# fails and dispatching a *real* review path (no AGY_REVIEW_SELFTEST), then showing that the run
# stopped before the relay: agy writes agy.log, and the relay writes result.json and final.txt, so
# the absence of all three is the absence of a model invocation.
echo
echo 'review-containment-check: fail-closed assertions (bwrap deliberately unavailable)'

# Run ids are second-granular, so a fast pair of runs can share an artifacts directory and the
# self-test's own result.json would masquerade as evidence that the model ran. These artifacts are
# disposable and gitignored; drop them so the next assertion measures only the fail-closed run.
RUN_ARTIFACTS="$(printf '%s\n' "$OUT" | sed -n 's/^agy-review: output  //p')"
case "$RUN_ARTIFACTS" in
  "$REPO_ROOT/docs/launch/review/artifacts/"?*) rm -rf "$RUN_ARTIFACTS" ;;
esac

STUB_BIN="$(mktemp -d)/bin"
mkdir -p "$STUB_BIN"
printf '#!/bin/sh\nexit 1\n' > "$STUB_BIN/bwrap"
chmod +x "$STUB_BIN/bwrap"

FC_OUT=""
FC_STATUS=0
FC_OUT="$(PATH="$STUB_BIN:$PATH" "$REPO_ROOT/scripts/agy-review.sh" "$RANGE_ARG" 2>&1)" || FC_STATUS=$?
printf '%s\n' "$FC_OUT" | sed 's/^/  | /'
rm -rf "$(dirname "$STUB_BIN")"

if [ "$FC_STATUS" -eq 5 ]; then
  pass 'a real review without bwrap exits 5 UNAVAILABLE'
else
  fail "a real review without bwrap exited $FC_STATUS, expected 5 UNAVAILABLE"
fi

if printf '%s\n' "$FC_OUT" | grep -q 'UNAVAILABLE — read-only containment could not be established'; then
  pass 'the refusal names containment, not a missing verdict'
else
  fail 'the refusal did not name containment as the reason'
fi

if printf '%s\n' "$FC_OUT" | grep -qi 'fallback\|permission-based'; then
  fail 'a real review fell back to permission-only containment'
else
  pass 'no permission-only fallback was used on the real review path'
fi

FC_ARTIFACTS="$(printf '%s\n' "$FC_OUT" | sed -n 's/^agy-review: output  //p')"
if [ -n "$FC_ARTIFACTS" ] && [ ! -e "$FC_ARTIFACTS/agy.log" ] \
   && [ ! -e "$FC_ARTIFACTS/result.json" ] && [ ! -e "$FC_ARTIFACTS/final.txt" ]; then
  pass 'the model was never invoked (no agy.log, result.json or final.txt)'
else
  fail "model-invocation artifacts exist under $FC_ARTIFACTS"
fi

if printf '%s\n' "$FC_OUT" | grep -qE '^VERDICT: '; then
  fail 'a verdict was produced without strong containment'
else
  pass 'no verdict can be produced without strong containment'
fi

FC_TREE="$(printf '%s\n' "$FC_OUT" | sed -n 's/^agy-review: tree    //p')"
FC_SCRATCH="$(printf '%s\n' "$FC_OUT" | sed -n 's/^agy-review: scratch //p')"
[ -n "$FC_TREE" ] && [ ! -e "$FC_TREE" ] && pass 'refused run cleaned up its worktree' \
  || fail "refused run left a worktree: $FC_TREE"
[ -n "$FC_SCRATCH" ] && [ ! -e "$FC_SCRATCH" ] && pass 'refused run cleaned up its scratch' \
  || fail "refused run left a scratch directory: $FC_SCRATCH"

FINAL_STATE="$(git -C "$REPO_ROOT" status --porcelain)"
if [ "$BEFORE" = "$FINAL_STATE" ]; then
  pass 'live repository is still unchanged after the fail-closed run'
else
  fail 'live repository changed during the fail-closed run'
fi

# --- fail-closed: bwrap runs but the read-only bind is not established -----------------------------
# "bwrap exists and starts" is not the property that matters. This stub accepts and ignores the bind
# flags, honours --chdir, and execs the command unsandboxed — exactly the shape of a bwrap that
# silently dropped a bind. The dispatcher's own containment probe must catch that and refuse.
echo
echo 'review-containment-check: fail-closed assertions (bwrap starts, binds not established)'

STUB2_DIR="$(mktemp -d)"
cat > "$STUB2_DIR/bwrap" <<'STUB'
#!/bin/sh
d=""
while [ $# -gt 0 ]; do
  case "$1" in
    --chdir) d="$2"; shift 2 ;;
    --dev-bind|--ro-bind|--bind) shift 3 ;;
    --die-with-parent) shift ;;
    *) break ;;
  esac
done
[ -n "$d" ] && cd "$d"
exec "$@"
STUB
chmod +x "$STUB2_DIR/bwrap"

NB_OUT=""
NB_STATUS=0
NB_OUT="$(PATH="$STUB2_DIR:$PATH" "$REPO_ROOT/scripts/agy-review.sh" "$RANGE_ARG" 2>&1)" || NB_STATUS=$?
printf '%s\n' "$NB_OUT" | sed 's/^/  | /'
rm -rf "$STUB2_DIR"

if [ "$NB_STATUS" -eq 5 ]; then
  pass 'a bwrap that does not actually bind read-only exits 5 UNAVAILABLE'
else
  fail "expected 5 UNAVAILABLE from an ineffective bwrap, got $NB_STATUS"
fi

if printf '%s\n' "$NB_OUT" | grep -q 'containment probe failed'; then
  pass 'the containment probe, not the bwrap presence check, caught it'
else
  fail 'the containment probe did not catch an ineffective bwrap'
fi

NB_ARTIFACTS="$(printf '%s\n' "$NB_OUT" | sed -n 's/^agy-review: output  //p')"
if [ -n "$NB_ARTIFACTS" ] && [ ! -e "$NB_ARTIFACTS/agy.log" ] \
   && [ ! -e "$NB_ARTIFACTS/result.json" ] && [ ! -e "$NB_ARTIFACTS/final.txt" ]; then
  pass 'the model was never invoked on the ineffective-bwrap path'
else
  fail "model-invocation artifacts exist under $NB_ARTIFACTS"
fi

FINAL_STATE="$(git -C "$REPO_ROOT" status --porcelain)"
if [ "$BEFORE" = "$FINAL_STATE" ]; then
  pass 'live repository is still unchanged after every refusal path'
else
  fail 'live repository changed during the ineffective-bwrap run'
fi

echo
if [ "$FAILURES" -eq 0 ]; then
  printf 'review-containment-check: PASS — the review worktree is not writable by the reviewer\n'
  exit 0
fi
printf 'review-containment-check: FAIL — %s assertion(s) failed\n' "$FAILURES" >&2
exit 1
