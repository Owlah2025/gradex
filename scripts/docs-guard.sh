#!/usr/bin/env bash
#
# docs-guard.sh — check the documentation invariants the launch protocol relies on.
#
# WHY
#   Since 2026-07-23 this check has been run by hand at the close of every day
#   and reconstructed from memory each time, which means its result could not be
#   reproduced by a reviewer or by CI. This script is that check, written down.
#
# WHAT IT CHECKS
#   1. Canonical documents exist. The launch protocol names specific files as
#      sources of truth; a missing one silently changes what "read the sources"
#      means.
#   2. Relative Markdown links resolve, including "#anchor" fragments. A dead
#      link in a design document is how a locked decision quietly disappears.
#   3. Locked design references resolve. Documents cite DECISIONS.md anchors and
#      launch-gate identifiers; a reference to one that does not exist is a
#      claim with nothing behind it.
#   4. Daily records carry the required structure — a State line with a known
#      value — so a record cannot be half-written and still look closed.
#   5. Applied migration files are unmodified. 0001_init is applied to real
#      databases; editing it makes an existing database and a fresh one differ
#      while both report the same schema version.
#   6. Generated artifacts are not committed.
#
# WHAT IT DOES NOT CHECK
#   Prose quality, whether a decision is correct, or whether documentation
#   matches implementation. Those need a reader.
#
# USAGE
#   scripts/docs-guard.sh [repo-root]

set -euo pipefail

ROOT="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
cd "$ROOT"

fail=0
note() { printf '%s\n' "$*" >&2; }
problem() { note "docs-guard: $*"; fail=1; }

# --- 1. canonical documents -------------------------------------------------
REQUIRED_DOCS=(
  "CLAUDE.md"
  "AGENTS.md"
  "docs/PRD.md"
  "docs/DECISIONS.md"
  "docs/BUSINESS_RULES.md"
  "docs/LAUNCH_GATES.md"
  "docs/launch/PLAN.md"
  "docs/launch/STATUS.md"
  "docs/launch/SLICES.md"
  "docs/launch/M1_ARCHITECTURE_BASELINE.md"
)
for doc in "${REQUIRED_DOCS[@]}"; do
  [ -f "$doc" ] || problem "required document is missing: $doc"
done

# --- 2/3. links, anchors, and locked references -----------------------------
python3 - "$ROOT" <<'PY' || fail=1
import os, re, sys, subprocess

root = sys.argv[1]
os.chdir(root)

tracked = [
    p for p in subprocess.run(
        ["git", "ls-files", "*.md"], capture_output=True, text=True, check=True
    ).stdout.splitlines()
    # .agents/ and .claude/ hold vendored tooling, not Gradex documentation.
    # Their contents are not ours to keep link-clean.
    if p and not p.startswith((".agents/", ".claude/", "frontend/node_modules/"))
]

FENCE = re.compile(r"^```.*?^```", re.M | re.S)
INLINE_CODE = re.compile(r"`[^`\n]*`")

def prose(markdown: str) -> str:
    """Drop code so a snippet like exporters[kind](record) is not read as a link."""
    return INLINE_CODE.sub("", FENCE.sub("", markdown))

def slugify(heading: str) -> str:
    s = heading.strip().lower()
    s = re.sub(r"`|\*|_", "", s)
    s = re.sub(r"[^a-z0-9\- ]", "", s)
    return s.replace(" ", "-")

def anchors(path: str) -> set:
    try:
        text = open(path, encoding="utf-8").read()
    except OSError:
        return set()
    return {slugify(h) for h in re.findall(r"^#{1,6}\s+(.*)$", text, re.M)}

anchor_cache = {}
failures = 0

LINK = re.compile(r"\[[^\]]*\]\(([^)\s]+)\)")

for doc in tracked:
    try:
        raw = open(doc, encoding="utf-8").read()
    except OSError:
        continue
    text = prose(raw)

    for target in LINK.findall(text):
        if target.startswith(("http://", "https://", "mailto:")):
            continue
        path, _, fragment = target.partition("#")

        resolved = doc if path == "" else os.path.normpath(
            os.path.join(os.path.dirname(doc), path))

        if not os.path.exists(resolved):
            print(f"docs-guard: {doc}: link target does not exist: {target}", file=sys.stderr)
            failures += 1
            continue

        if fragment and resolved.endswith(".md"):
            if resolved not in anchor_cache:
                anchor_cache[resolved] = anchors(resolved)
            if fragment not in anchor_cache[resolved]:
                print(f"docs-guard: {doc}: link anchor does not resolve: {target}", file=sys.stderr)
                failures += 1

    # Locked design references: a cited decision must exist in DECISIONS.md.
    if os.path.exists("docs/DECISIONS.md"):
        decisions = open("docs/DECISIONS.md", encoding="utf-8").read()
        defined = set(re.findall(r"\bD-\d{3}\b", decisions))
        for ref in set(re.findall(r"\bD-\d{3}\b", text)):
            if doc != "docs/DECISIONS.md" and ref not in defined:
                print(f"docs-guard: {doc}: cites {ref}, which DECISIONS.md does not define",
                      file=sys.stderr)
                failures += 1

    # Launch-gate identifiers must exist in the register.
    if os.path.exists("docs/LAUNCH_GATES.md"):
        gates = open("docs/LAUNCH_GATES.md", encoding="utf-8").read()
        defined = set(re.findall(r"\bLG-\d{3}\b", gates))
        for ref in set(re.findall(r"\bLG-\d{3}\b", text)):
            if doc != "docs/LAUNCH_GATES.md" and ref not in defined:
                print(f"docs-guard: {doc}: cites {ref}, which LAUNCH_GATES.md does not define",
                      file=sys.stderr)
                failures += 1

sys.exit(1 if failures else 0)
PY

# --- 4. daily record structure ----------------------------------------------
shopt -s nullglob
for record in docs/launch/daily/*.md; do
  state_line=$(grep -m1 '^> State:' "$record" || true)
  if [ -z "$state_line" ]; then
    problem "$record: no '> State:' line"
    continue
  fi
  case "${state_line#> State: }" in
    PLANNED|IN_PROGRESS|CLOSED) ;;
    *) problem "$record: state must be PLANNED, IN_PROGRESS, or CLOSED; found '${state_line#> State: }'" ;;
  esac
done
shopt -u nullglob

# --- 5. applied migrations are immutable ------------------------------------
# 0001_init is applied to real databases. Editing it makes an existing database
# and a freshly migrated one disagree while both report the same version.
# Checksums recorded when the migration was declared applied. Update them only
# alongside a new migration that supersedes the old shape.
CHECKSUM_FILE="scripts/applied-migrations.sha256"
if [ -f "$CHECKSUM_FILE" ]; then
  if ! sha256sum --check --status "$CHECKSUM_FILE" 2>/dev/null; then
    problem "an applied migration file changed. 0001_init is already applied to"
    note "  real databases, so editing it makes an existing database and a fresh"
    note "  one differ while both report the same schema version. Add a new"
    note "  migration instead. If this change is deliberate and no database has"
    note "  the old shape, regenerate $CHECKSUM_FILE and say so in review."
  fi
else
  problem "$CHECKSUM_FILE is missing; applied migrations are unprotected"
fi

# --- 6. generated artifacts are not committed -------------------------------
# Named explicitly rather than by glob: "Wireframe review_ ....zip" is
# committed design source, not build output, and a broad *.zip rule would
# demand its deletion.
GENERATED_ARTIFACTS=(
  "gradex-spec-review.zip"
  "frontend/tsconfig.tsbuildinfo"
  "backend/tmp"
)
for artifact in "${GENERATED_ARTIFACTS[@]}"; do
  if git ls-files --error-unmatch "$artifact" >/dev/null 2>&1; then
    problem "generated artifact is committed: $artifact"
  fi
done

if [ "$fail" -eq 0 ]; then
  echo "docs-guard: ok ($(git ls-files '*.md' | wc -l | tr -d ' ') Markdown files checked)"
fi
exit "$fail"
