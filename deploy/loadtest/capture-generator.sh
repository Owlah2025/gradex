#!/usr/bin/env bash

set -euo pipefail

note() { printf 'gradex-generator-capture: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

main() {
  [ "$#" = 1 ] || die "usage: capture-generator.sh OUTPUT_DIRECTORY"
  local output pid duration interval deadline now fd_count cpu memory proc_before proc_after total_before total_after cpu_count fd
  for tool in awk date find mkdir nproc readlink sleep tr wc; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
  done
  pid="${GRADEX_LOADTEST_GENERATOR_PID:-}"
  [[ "$pid" =~ ^[1-9][0-9]*$ ]] || die "GRADEX_LOADTEST_GENERATOR_PID must identify the exact generator process"
  [ -r "/proc/$pid/status" ] || die "generator process is not readable"
  output="$(readlink -m -- "$1")"
  [ "$output" != / ] || die "output directory cannot be root"
  [ ! -e "$output" ] && [ ! -L "$output" ] || die "output directory already exists"
  mkdir -m 700 -- "$output"
  duration="${GRADEX_LOADTEST_CAPTURE_SECONDS:-90}"
  [[ "$duration" =~ ^[1-9][0-9]*$ ]] && [ "$duration" -le 900 ] || die "capture duration must be between 1 and 900 seconds"
  interval=5
  printf 'observed_at\tpid\tcpu_percent\tmemory_bytes\topen_fd_count\tfd_limit\n' >"$output/generator-metrics.tsv"
  chmod 600 "$output/generator-metrics.tsv"
  deadline=$(( $(date +%s) + duration ))
  cpu_count="$(nproc)"
  proc_before="$(awk '{sub(/^.*\) /, ""); print $12 + $13; exit}' "/proc/$pid/stat")"
  total_before="$(awk '/^cpu / {sum=0; for (i=2; i<=NF; i++) sum += $i; print sum; exit}' /proc/stat)"
  while [ "$(date +%s)" -lt "$deadline" ] && [ -r "/proc/$pid/status" ]; do
    sleep "$interval"
    now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    fd_count="$(find "/proc/$pid/fd" -mindepth 1 -maxdepth 1 -type l 2>/dev/null | wc -l | tr -d ' ')"
    fd="$(awk '/^Max open files:/ {print $4; exit}' "/proc/$pid/limits" 2>/dev/null || true)"
    proc_after="$(awk '{sub(/^.*\) /, ""); print $12 + $13; exit}' "/proc/$pid/stat" 2>/dev/null || true)"
    total_after="$(awk '/^cpu / {sum=0; for (i=2; i<=NF; i++) sum += $i; print sum; exit}' /proc/stat)"
    cpu="$(awk -v before="${proc_before:-0}" -v after="${proc_after:-0}" -v total_before="$total_before" -v total_after="$total_after" -v cores="$cpu_count" 'BEGIN { total=total_after-total_before; process=after-before; if (total <= 0 || process < 0) print "0.00"; else printf "%.2f", 100 * process / total * cores }')"
    proc_before="${proc_after:-0}"
    total_before="$total_after"
    memory="$(awk '/^VmRSS:/ {print $2 * 1024; exit}' "/proc/$pid/status" 2>/dev/null || true)"
    printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$now" "$pid" "$cpu" "${memory:-0}" "$fd_count" "${fd:-unknown}" >>"$output/generator-metrics.tsv"
  done
  note "captured bounded generator CPU proxy, memory, file-descriptor pressure, and limits for PID $pid"
}

main "$@"
