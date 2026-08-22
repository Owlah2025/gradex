#!/usr/bin/env bash

set -euo pipefail

note() { printf 'gradex-loadtest-capture: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

cpu_sample() {
  awk '/^cpu / { idle=$5+$6; total=0; for (i=2; i<=NF; i++) total+=$i; print total, idle; exit }' /proc/stat
}

memory_value() {
  awk -v key="$1" '$1 == key":" { print $2 * 1024; exit }' /proc/meminfo
}

main() {
  [ "$#" = 1 ] || die "usage: capture-server.sh OUTPUT_DIRECTORY"
  local output project duration interval started deadline now previous_total previous_idle total idle
  local delta_total delta_idle cpu_percent mem_total mem_available swap_total swap_free service id
  local -a container_ids
  for tool in awk date df docker head jq mkdir readlink sleep; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
  done
  output="$(readlink -m -- "$1")"
  [ "$output" != / ] || die "output directory cannot be root"
  [ ! -e "$output" ] && [ ! -L "$output" ] || die "output directory already exists"
  mkdir -m 700 -- "$output"
  project="${GRADEX_LOADTEST_COMPOSE_PROJECT:-gradex-staging}"
  [[ "$project" =~ ^[a-z0-9][a-z0-9_-]{2,62}$ ]] || die "compose project is invalid"
  duration="${GRADEX_LOADTEST_CAPTURE_SECONDS:-90}"
  [[ "$duration" =~ ^[1-9][0-9]*$ ]] && [ "$duration" -le 900 ] ||
    die "capture duration must be between 1 and 900 seconds"
  interval=5

  container_ids=()
  for service in postgres redis minio api worker frontend edge; do
    id="$(docker ps --filter "label=com.docker.compose.project=$project" \
      --filter "label=com.docker.compose.service=$service" --quiet | head -1)"
    [ -z "$id" ] || container_ids+=("$id")
  done
  [ "${#container_ids[@]}" -gt 0 ] || die "no running Gradex containers were found for project $project"

  printf 'observed_at\tcpu_percent\tmemory_used_bytes\tmemory_total_bytes\tswap_used_bytes\tswap_total_bytes\tdisk_used_bytes\tdisk_total_bytes\n' \
    >"$output/host-metrics.tsv"
  chmod 600 "$output/host-metrics.tsv"
  : >"$output/container-stats.jsonl"
  chmod 600 "$output/container-stats.jsonl"

  read -r previous_total previous_idle < <(cpu_sample)
  started="$(date +%s)"
  deadline=$((started + duration))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    sleep "$interval"
    now="$(date +%s)"
    read -r total idle < <(cpu_sample)
    delta_total=$((total - previous_total))
    delta_idle=$((idle - previous_idle))
    cpu_percent="$(awk -v total="$delta_total" -v idle="$delta_idle" 'BEGIN { if (total <= 0) print "0.00"; else printf "%.2f", 100 * (total-idle) / total }')"
    previous_total="$total"
    previous_idle="$idle"
    mem_total="$(memory_value MemTotal)"
    mem_available="$(memory_value MemAvailable)"
    swap_total="$(memory_value SwapTotal)"
    swap_free="$(memory_value SwapFree)"
    read -r _ disk_total disk_used _ < <(df -P -B1 / | awk 'NR == 2 {print $1, $2, $3, $4}')
    printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
      "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$cpu_percent" "$((mem_total - mem_available))" "$mem_total" \
      "$((swap_total - swap_free))" "$swap_total" "$disk_used" "$disk_total" >>"$output/host-metrics.tsv"
    docker stats --no-stream --format '{{json .}}' "${container_ids[@]}" |
      jq --compact-output --arg observed_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" '. + {observed_at:$observed_at}' \
      >>"$output/container-stats.jsonl"
    [ "$now" -lt "$deadline" ] || break
  done

  for service in postgres redis minio api worker frontend edge; do
    id="$(docker ps --filter "label=com.docker.compose.project=$project" \
      --filter "label=com.docker.compose.service=$service" --quiet | head -1)"
    [ -n "$id" ] || continue
    docker inspect --format \
      '{"service":"'"$service"'","status":"{{.State.Status}}","restart_count":{{.RestartCount}},"health":"{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}"}' \
      "$id" >"$output/$service-state.json"
    docker logs --since "$((duration + 30))s" --tail 500 "$id" >"$output/$service.log" 2>&1
    chmod 600 "$output/$service-state.json" "$output/$service.log"
  done
  note "captured bounded host, container, disk, and service evidence without changing the application"
}

main "$@"
