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

echo
if [ "$FAILURES" -eq 0 ]; then
  printf 'review-containment-check: PASS — the review worktree is not writable by the reviewer\n'
  exit 0
fi
printf 'review-containment-check: FAIL — %s assertion(s) failed\n' "$FAILURES" >&2
exit 1
