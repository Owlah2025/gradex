#!/usr/bin/env bash

# Proves the restored-database invariant compares the restore against what the
# source actually held, rather than assuming every record class is populated.
#
# The regression this guards was observed on the real Hostinger staging host: the
# invariant required all five record classes to be non-empty, but staging
# legitimately holds two entitlements, both REVOKED, and therefore zero ACTIVE
# invitation-sourced entitlements. A byte-faithful restore of that database failed
# verification. Accepting a legitimate zero must not become accepting anything, so
# the mismatch and missing-table cases below must still fail.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

die() {
  printf 'hostinger-restore-invariants: %s\n' "$*" >&2
  exit 1
}

note() { :; }

RESTORED_STATE=""
MISSING_TABLE=""

docker() {
  case "$*" in
    exec*psql*to_regclass*)
      local table
      table="$(printf '%s' "$*" | sed -n "s/.*to_regclass('public\.\([a-z_]*\)').*/\1/p")"
      # A table the restore lost answers NULL, which psql prints as an empty line.
      [ "$table" = "$MISSING_TABLE" ] && return 0
      printf '%s\n' "$table"
      ;;
    exec*psql*)
      printf '%s\n' "$RESTORED_STATE"
      ;;
    *)
      printf 'unexpected docker call: %s\n' "$*" >&2
      return 99
      ;;
  esac
}

extract() {
  awk -v name="$1" '
    $0 ~ "^" name "\\(\\)" { capture = 1 }
    capture { print }
    capture && /^}/ { exit }
  ' "$ROOT/deploy/hostinger/host.sh"
}

eval "$(extract restored_database_state)"
eval "$(extract assert_restored_database_invariants)"

# The exact live staging shape that used to fail: zero ACTIVE invitation-sourced
# entitlements, faithfully restored, must PASS.
RESTORED_STATE='28|false|8|3|2|0|2'
assert_restored_database_invariants target-id '28|false' '8|3|2|0|2' ||
  die "a faithful restore with a legitimate zero count was rejected"

# Every class zero is still a faithful restore of an empty-but-migrated source.
RESTORED_STATE='28|false|0|0|0|0|0'
assert_restored_database_invariants target-id '28|false' '0|0|0|0|0' ||
  die "a faithful restore of an empty source was rejected"

# `die` exits, so every negative case runs in a subshell.

# Negative: a truncated restore must FAIL rather than pass as "zero is allowed".
if (
  RESTORED_STATE='28|false|0|0|0|0|0'
  assert_restored_database_invariants target-id '28|false' '8|3|2|0|2' >/dev/null 2>&1
); then
  die "an empty restore of a populated source was accepted"
fi

# Negative: a partial restore of a single class must FAIL.
if (
  RESTORED_STATE='28|false|8|3|2|0|1'
  assert_restored_database_invariants target-id '28|false' '8|3|2|0|2' >/dev/null 2>&1
); then
  die "a partial restore was accepted"
fi

# Negative: extra rows are a mismatch too; the restore is not the recorded source.
if (
  RESTORED_STATE='28|false|9|3|2|0|2'
  assert_restored_database_invariants target-id '28|false' '8|3|2|0|2' >/dev/null 2>&1
); then
  die "a restore holding more records than the source was accepted"
fi

# Negative: schema mismatch must still FAIL.
if (
  RESTORED_STATE='27|false|8|3|2|0|2'
  assert_restored_database_invariants target-id '28|false' '8|3|2|0|2' >/dev/null 2>&1
); then
  die "a restore at the wrong schema version was accepted"
fi

# Negative: a dirty schema must still FAIL.
if (
  RESTORED_STATE='28|true|8|3|2|0|2'
  assert_restored_database_invariants target-id '28|false' '8|3|2|0|2' >/dev/null 2>&1
); then
  die "a restore with a dirty schema was accepted"
fi

# Negative: a missing critical table must FAIL even when counts would agree.
RESTORED_STATE='28|false|8|3|2|0|2'
if (
  MISSING_TABLE=entitlements
  assert_restored_database_invariants target-id '28|false' '8|3|2|0|2' >/dev/null 2>&1
); then
  die "a restore missing the entitlements table was accepted"
fi

if (
  MISSING_TABLE=accounts
  assert_restored_database_invariants target-id '28|false' '8|3|2|0|2' >/dev/null 2>&1
); then
  die "a restore missing the accounts table was accepted"
fi

# A snapshot captured before counts were recorded still validates its schema and
# its tables, and says so rather than silently claiming a count comparison.
RESTORED_STATE='28|false|8|3|2|0|2'
assert_restored_database_invariants target-id '28|false' '' ||
  die "a pre-record-count snapshot was rejected"

if (
  MISSING_TABLE=courses
  assert_restored_database_invariants target-id '28|false' '' >/dev/null 2>&1
); then
  die "a pre-record-count snapshot missing a critical table was accepted"
fi

printf '%s\n' \
  'hostinger-restore-invariants: legitimate zero, empty-source, truncation, partial, surplus, schema, dirty, missing-table, and legacy-snapshot checks passed'
