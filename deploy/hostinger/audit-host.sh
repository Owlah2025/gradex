#!/usr/bin/env bash

set -euo pipefail

note() { printf 's12-host-audit: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

for tool in docker ss timedatectl; do
  command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
done

[ -r /etc/os-release ] || die "OS release metadata is unavailable"
# shellcheck disable=SC1091
. /etc/os-release
case "${ID:-}:${VERSION_ID:-}" in
  ubuntu:22.04|ubuntu:24.04) ;;
  *) die "Hostinger host must run supported Ubuntu 22.04 or 24.04 LTS" ;;
esac

operator="${SUDO_USER:-}"
operator_uid="${SUDO_UID:-0}"
[ "$(id -u)" -eq 0 ] && [ -n "$operator" ] && [ "$operator_uid" -ne 0 ] ||
  die "run with sudo from the non-root operational user"
id -nG "$operator" | tr ' ' '\n' | grep --fixed-strings --line-regexp --quiet docker ||
  die "operational user is not in the docker group"
timedatectl show --property=NTPSynchronized --value | grep --fixed-strings --line-regexp --quiet yes ||
  die "host time synchronization is not active"

if command -v sshd >/dev/null 2>&1; then
  ssh_effective="$(sshd -T 2>/dev/null || true)"
  grep --fixed-strings --line-regexp --quiet 'passwordauthentication no' <<<"$ssh_effective" ||
    die "SSH password authentication is not disabled"
  grep --extended-regexp --line-regexp --quiet 'permitrootlogin (no|prohibit-password)' <<<"$ssh_effective" ||
    die "SSH root password login is not disabled"
fi

if command -v ufw >/dev/null 2>&1; then
  ufw_status="$(ufw status verbose 2>/dev/null)"
  grep --fixed-strings --quiet 'Status: active' <<<"$ufw_status" || die "UFW is not active"
  grep --extended-regexp --quiet 'Default: deny \(incoming\)' <<<"$ufw_status" ||
    die "UFW must deny incoming traffic by default"
else
  die "UFW is required unless an equivalent host firewall is independently evidenced"
fi

if ss -lntH | awk '{print $4}' | grep --extended-regexp --quiet '(^|:)(3000|5432|6379|8080)$'; then
  die "an application, PostgreSQL, or Redis port is listening on a host interface"
fi

docker info >/dev/null 2>&1 || die "Docker is not reachable"
note "supported OS, non-root operator, SSH posture, firewall, time sync, Docker, and private service ports passed"
