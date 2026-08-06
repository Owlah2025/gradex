#!/usr/bin/env bash
#
# s5-convergence-gate.sh — prove the S5 commit range contains exactly the
# authorized history, and that the working tree is clean against its baseline.
#
# WHY
#   T077 originally asserted `git rev-list --count 9c8348a..HEAD` must equal 3.
#   That numeral was introduced by e7736077 — before any CI-produced evidence
#   existed — and it encoded a prediction: that every remaining S5 task would
#   land inside one final implementation commit. The prediction is impossible.
#   T075's evidence is a GitHub Actions artifact and T078's is hosted CI plus an
#   independent verdict, and neither can exist until a commit is pushed. So the
#   range must legitimately grow, and a fixed count fails for the wrong reason.
#
#   A raw count was also weaker than it looked: it cannot distinguish an
#   authorized commit from an unauthorized one of the same number. This gate
#   replaces the numeral with what the numeral was standing in for — membership
#   in an authorized set, plus a changed-path boundary per class. See D-067.
#
# WHAT IT CHECKS
#   1. The three accepted commits are present at their exact SHAs, in order.
#   2. Every later commit matches exactly one authorized subject class.
#      Classes: GATE, WORKFLOW, CI_STABILIZATION, T076_EVIDENCE, CLOSURE,
#      TIER3_REMEDIATION, TIER3_EVIDENCE, CONVERGENCE.
#   3. Every later commit's changed paths lie inside that class's allowlist.
#   4. No commit in the range touches S5 production scope after the
#      implementation commit. An authorized subject is not a licence to edit a
#      handler. Exactly one exception exists, recorded as (class, exact path)
#      pairs: TIER3_REMEDIATION carries the FR-017/BR-102 playback ceilings the
#      implementation commit shipped without, and a rate limit has nowhere to
#      live but production code. See D-071.
#   5. No merge commit is in the range.
#   6. The working tree is clean, nothing is staged, and the NUL-delimited
#      porcelain status is byte-identical to the recorded baseline (D-059).
#
# WHAT IT DOES NOT CHECK
#   Whether a task is genuinely finished, whether CI passed, or whether a review
#   verdict is sound. Those are T075/T076/T078 and a human reviewer.
#
# BOOTSTRAP
#   The commit that introduces this gate cannot require its own SHA, so accepted
#   commits are pinned by SHA and later commits are classified by subject and
#   path. Use --history-only while the gate correction is still uncommitted:
#   classification runs, and the clean-tree assertions — which a work-in-progress
#   tree cannot satisfy — are skipped and reported as skipped.
#
# USAGE
#   scripts/s5-convergence-gate.sh [--history-only] [--base <sha>] [repo-root]
#
#   S5_STATUS_BASELINE  path to the NUL-delimited porcelain baseline recorded
#                       immediately before the implementation commit.
#                       Default: /tmp/gradex-s5-status-baseline.z (quickstart).
#                       The file is read only and never written.

set -euo pipefail

BASE_DEFAULT="9c8348a19b5375b5f4e3eb4d6556426b3323a02c"
BASE="$BASE_DEFAULT"
HISTORY_ONLY=0
ROOT=""

while [ $# -gt 0 ]; do
  case "$1" in
    --history-only) HISTORY_ONLY=1; shift ;;
    --base) BASE="$2"; shift 2 ;;
    *) ROOT="$1"; shift ;;
  esac
done

if [ -z "$ROOT" ]; then
  ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fi
cd "$ROOT"

BASELINE="${S5_STATUS_BASELINE:-/tmp/gradex-s5-status-baseline.z}"

fail=0
note() { printf '%s\n' "$*" >&2; }
problem() { note "s5-gate: $*"; fail=1; }

# --- the accepted commits, pinned exactly ------------------------------------
# Order matters: these are the first three commits of the S5 range.
ACCEPTED_SHAS=(
  "f81d8327b7adf15586730cc85c8c89198c857e23"
  "e7736077bb8a2158a1fa4258c5a3e7fc736cdd56"
  "5cc8ede07cc0501f34b1e82b789e0e2d63873a78"
)
ACCEPTED_LABELS=(
  "clean-tree gate correction (D-059)"
  "D-060 reconciliation"
  "protected-learning implementation"
)

# --- S5 production scope, forbidden after the implementation commit ----------
# A path here fails regardless of how well-formed the commit subject is. This is
# the clause that makes an authorized subject insufficient on its own.
is_production_path() {
  case "$1" in
    backend/internal/learning/*|\
    backend/internal/httpapi/*|\
    backend/internal/entitlement/*|\
    backend/internal/media/*|\
    backend/internal/ratelimit/*|\
    backend/internal/db/migrations/*|\
    backend/internal/db/schema.go|\
    backend/cmd/api/*|\
    frontend/src/app/*|\
    frontend/src/components/*)
      # Test files are not production behaviour. Evidence classes may add or
      # adjust their own detectors without reopening the implementation.
      case "$1" in
        *_test.go|*.test.ts|*.test.tsx) return 1 ;;
        *) return 0 ;;
      esac
      ;;
    *) return 1 ;;
  esac
}

# --- the single authorized production exception -------------------------------
# Clause 4 forbids production edits after the implementation commit. One exception exists,
# and it is deliberately expressed as (class, exact path) pairs rather than as a hole in
# is_production_path: TIER3_REMEDIATION carries the FR-017/BR-102 playback ceilings that the
# implementation commit omitted, and a rate limit cannot be implemented anywhere except in
# production code. No other class may use it, and no directory glob appears in it.
#
# production_exception_allowed <class> <path>
production_exception_allowed() {
  [ "$1" = "TIER3_REMEDIATION" ] || return 1
  case "$2" in
    backend/internal/ratelimit/policy.go|\
    backend/internal/httpapi/learning_handlers.go|\
    backend/internal/httpapi/learning_rate_limit.go|\
    backend/internal/httpapi/learning_foundation.go|\
    backend/cmd/api/main.go) return 0 ;;
  esac
  return 1
}

# --- authorized classes for commits after the implementation commit ----------
# classify_subject <subject> -> prints class name, or nothing when unclassified.
classify_subject() {
  case "$1" in
    "docs(s5): replace fixed convergence count with semantic gate"*) printf 'GATE' ;;
    # A gate correction may be made more than once — D-067 was itself amended to add
    # CI_STABILIZATION after hosted CI exposed defects the forecast classes missed. Each
    # correction carries its own exact subject rather than a `docs(s5): *` prefix, which would
    # classify any documentation commit as a gate change.
    "docs(s5): authorize narrow CI stabilization class"*)             printf 'GATE' ;;
    "docs(s5): authorize separate S5 closures")                       printf 'GATE' ;;
    "docs(s5): authorize production-origin T076 evidence")             printf 'GATE' ;;
    "docs(s5): authorize T077 convergence closure")                   printf 'GATE' ;;
    "docs(s5): authorize Tier 3 playback remediation")                printf 'GATE' ;;
    "fix(ci): validate S5 rendered evidence workflow"*)               printf 'WORKFLOW' ;;
    # T075 and T076 are independently evidenced tasks — T075 on a verified hosted artifact, T076 on
    # its own time-to-first-frame measurement — so each is closed by its own truthful commit. The
    # former combined subject `docs(s5): close T075 and T076` is deliberately gone: it was false for
    # a T075-only closure, and there was no honest way to phrase one under it.
    #
    # These two are matched EXACTLY, with no trailing glob. A prefix would re-admit both the obsolete
    # combined subject and near-matches like `docs(s5): close T075 evidence`, which is precisely what
    # the exactness is here to reject.
    "docs(s5): close T075")                                           printf 'CLOSURE' ;;
    "docs(s5): close T076")                                           printf 'CLOSURE' ;;
    # T077 verifies this gate on the final implementation history and closes independently of T078,
    # which needs hosted CI on a frozen head plus an independent Tier 3 verdict the builder can never
    # supply. Exact match, no glob: a prefix would admit a combined subject falsely claiming T078.
    "docs(s5): close T077")                                           printf 'CLOSURE' ;;
    "docs(s5): record convergence and independent review"*)           printf 'CONVERGENCE' ;;
    "fix(ci): stabilize hosted S5 verification"*)                     printf 'CI_STABILIZATION' ;;
    "test(s5): add production-origin SC-001 evidence")                 printf 'T076_EVIDENCE' ;;
    # Independent Tier 3 review found playback issuance shipped without the FR-017/BR-102 rate
    # limits. Remediating that requires editing production scope after the implementation commit,
    # which every other class is forbidden to do -- so it gets its own class, its own exact-file
    # production carve-out (see production_exception_allowed), and its own review. Exact subjects,
    # no globs.
    "fix(s5): enforce playback issuance rate limits")                  printf 'TIER3_REMEDIATION' ;;
    "docs(s5): reconcile Tier 3 remediation evidence")                 printf 'TIER3_EVIDENCE' ;;
    "docs(s5): refresh T077 after Tier 3 remediation")                 printf 'TIER3_EVIDENCE' ;;
    *) : ;;
  esac
}

# path_allowed <class> <path>
path_allowed() {
  local class="$1" p="$2"
  case "$class" in
    GATE)
      case "$p" in
        docs/DECISIONS.md|\
        specs/007-protected-learning/tasks.md|\
        specs/007-protected-learning/quickstart.md|\
        scripts/s5-convergence-gate.sh) return 0 ;;
      esac
      ;;
    WORKFLOW)
      case "$p" in
        .github/workflows/ci.yml|\
        docs/launch/STATUS.md|\
        docs/launch/daily/*.md|\
        specs/007-protected-learning/tasks.md|\
        frontend/playwright.config.ts|\
        frontend/scripts/t075-evidence-manifest.mjs|\
        frontend/e2e/s5-viewport-evidence.spec.ts|\
        frontend/src/lib/api/e2e-infrastructure.ts) return 0 ;;
      esac
      ;;
    CLOSURE)
      # T076's spec, evidence wiring, and the records that cite the figures.
      # Deliberately no production path: if T076 uncovers a real defect it gets
      # its own reviewed class rather than hiding inside evidence closure.
      case "$p" in
        specs/007-protected-learning/tasks.md|\
        docs/launch/STATUS.md|\
        docs/launch/daily/*.md|\
        frontend/e2e/s5-playback-performance.spec.ts|\
        frontend/e2e/s5-viewport-evidence.spec.ts|\
        frontend/scripts/t075-evidence-manifest.mjs|\
        frontend/playwright.config.ts|\
        .github/workflows/ci.yml) return 0 ;;
      esac
      ;;
    T076_EVIDENCE)
      # SC-001 is a claim about the built application, so T076 measures `next build` output behind a
      # run-owned loopback proxy that reproduces the deployed same-origin frontend/API boundary. Four
      # exact files, no glob: the spec, the proxy, the Playwright config's opt-in production branch,
      # and the evidence job. Deliberately absent — `next.config.mjs` (production configuration),
      # `package.json`/`package-lock.json` (the proxy uses Node built-ins only), and every application
      # path.
      case "$p" in
        frontend/e2e/s5-playback-performance.spec.ts|\
        frontend/e2e/production-origin-proxy.mjs|\
        frontend/playwright.config.ts|\
        .github/workflows/ci.yml) return 0 ;;
      esac
      ;;
    CI_STABILIZATION)
      # Hosted CI exposed two defects the forecast classes did not cover: the seeder's TestMain
      # failing an ordinary `go test ./...`, and a failing evidence run being undiagnosable without
      # admin log access. This class authorizes exactly those files and nothing else. It is
      # deliberately an exact-file list rather than a directory glob — no `backend/cmd/e2e-seed/*`,
      # because that would quietly admit a future non-test file in the same directory.
      case "$p" in
        backend/cmd/e2e-seed/seed_test.go|\
        backend/cmd/e2e-seed/invocation_test.go|\
        .github/workflows/ci.yml|\
        frontend/scripts/t075-evidence-manifest.mjs) return 0 ;;
      esac
      ;;
    TIER3_REMEDIATION)
      # The exact files that carry the two playback ceilings, plus the hosted-CI package list
      # (M-1) and the executable evidence. Production files here are additionally gated by
      # production_exception_allowed, so a typo'd path fails twice rather than passing once.
      case "$p" in
        backend/internal/ratelimit/policy.go|\
        backend/internal/httpapi/learning_handlers.go|\
        backend/internal/httpapi/learning_rate_limit.go|\
        backend/internal/httpapi/learning_foundation.go|\
        backend/internal/httpapi/learning_foundation_test.go|\
        backend/internal/httpapi/learning_playback_rate_limit_test.go|\
        backend/cmd/api/main.go|\
        .github/workflows/ci.yml) return 0 ;;
      esac
      ;;
    TIER3_EVIDENCE)
      # Records only. No production path and no test path: the remediation itself is already
      # closed by TIER3_REMEDIATION, and reopening code under an evidence subject is exactly the
      # substitution this gate exists to prevent.
      case "$p" in
        specs/007-protected-learning/tasks.md|\
        docs/DECISIONS.md|\
        docs/launch/STATUS.md|\
        docs/launch/daily/*.md) return 0 ;;
      esac
      ;;
    CONVERGENCE)
      case "$p" in
        specs/007-protected-learning/tasks.md|\
        docs/DECISIONS.md|\
        docs/launch/STATUS.md|\
        docs/launch/daily/*.md|\
        docs/launch/review/*) return 0 ;;
      esac
      ;;
  esac
  return 1
}

# --- 1/2/3/4. history classification ----------------------------------------
mapfile -t RANGE < <(git log --format='%H' --reverse "${BASE}..HEAD")

if [ "${#RANGE[@]}" -lt "${#ACCEPTED_SHAS[@]}" ]; then
  problem "range ${BASE}..HEAD holds ${#RANGE[@]} commit(s); the ${#ACCEPTED_SHAS[@]} accepted commits must all be present"
fi

for i in "${!ACCEPTED_SHAS[@]}"; do
  expected="${ACCEPTED_SHAS[$i]}"
  actual="${RANGE[$i]:-<absent>}"
  if [ "$actual" != "$expected" ]; then
    problem "position $((i + 1)) of the S5 range must be ${expected} (${ACCEPTED_LABELS[$i]}); found ${actual}"
  fi
done

for idx in "${!RANGE[@]}"; do
  sha="${RANGE[$idx]}"
  # The accepted commits are pinned above; they are not re-classified, because
  # the implementation commit legitimately touches production.
  if [ "$idx" -lt "${#ACCEPTED_SHAS[@]}" ] && [ "$sha" = "${ACCEPTED_SHAS[$idx]}" ]; then
    continue
  fi

  subject="$(git log -1 --format='%s' "$sha")"
  class="$(classify_subject "$subject")"

  if [ -z "$class" ]; then
    problem "unclassified commit ${sha}: '${subject}' — no authorized class. Every commit after the implementation commit must belong to an authorized class (D-067)."
    continue
  fi

  while IFS= read -r p; do
    [ -n "$p" ] || continue
    if is_production_path "$p" && ! production_exception_allowed "$class" "$p"; then
      problem "commit ${sha} (${class}, '${subject}') changes S5 production scope: ${p}"
      continue
    fi
    if ! path_allowed "$class" "$p"; then
      problem "commit ${sha} (${class}, '${subject}') changes a path outside its class boundary: ${p}"
    fi
  done < <(git show --pretty=format: --name-only "$sha")
done

# --- 5. no merge commits -----------------------------------------------------
merges="$(git rev-list --merges "${BASE}..HEAD" || true)"
if [ -n "$merges" ]; then
  while IFS= read -r m; do
    [ -n "$m" ] || continue
    problem "merge commit is forbidden in the S5 range: ${m} '$(git log -1 --format='%s' "$m")'"
  done <<<"$merges"
fi

# --- 6. clean tree and baseline comparison (D-059) --------------------------
if [ "$HISTORY_ONLY" -eq 1 ]; then
  note "s5-gate: --history-only: clean-tree and baseline checks SKIPPED (bootstrap mode)"
else
  git diff --quiet || problem "working tree has unstaged changes"
  git diff --cached --quiet || problem "index has staged changes"

  if [ ! -f "$BASELINE" ]; then
    problem "recorded porcelain baseline is missing: ${BASELINE} (set S5_STATUS_BASELINE, or capture it per quickstart before implementation)"
  else
    final="$(mktemp)"
    trap 'rm -f "$final"' EXIT
    git status --porcelain=v1 -z --untracked-files=all >"$final"
    if ! cmp -s "$BASELINE" "$final"; then
      problem "final porcelain status is not byte-identical to the recorded baseline ${BASELINE}"
      note "s5-gate: baseline $(wc -c <"$BASELINE") byte(s) vs final $(wc -c <"$final") byte(s)"
      note "s5-gate: entries now present:"
      git status --porcelain=v1 --untracked-files=all | sed 's/^/  /' >&2
    fi
  fi
fi

if [ "$fail" -ne 0 ]; then
  note "s5-gate: FAILED"
  exit 1
fi

if [ "$HISTORY_ONLY" -eq 1 ]; then
  note "s5-gate: ok — ${#RANGE[@]} commit(s) classified, no merge commit (clean-tree checks skipped)"
else
  note "s5-gate: ok — ${#RANGE[@]} commit(s) classified, no merge commit, tree clean and equal to baseline"
fi
