#!/usr/bin/env bash
set -euo pipefail

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)"
COMPOSE_FILE="$ROOT/deploy/loadtest/compose.target.yml"
SNAP_MOUNT=/mnt/gradex-founder-ui
if [ -r "$SNAP_MOUNT/deploy/loadtest/compose.target.yml" ] &&
  [ "$(stat -Lc '%d:%i' "$COMPOSE_FILE")" = "$(stat -Lc '%d:%i' "$SNAP_MOUNT/deploy/loadtest/compose.target.yml")" ]; then
  COMPOSE_FILE="$SNAP_MOUNT/deploy/loadtest/compose.target.yml"
fi
umask 077
RENDERED="$(mktemp)"
trap 'rm -f -- "$RENDERED"' EXIT

die() {
  printf 'verify-target-compose: %s\n' "$*" >&2
  exit 1
}

command -v docker >/dev/null 2>&1 || die "docker is required"
command -v python3 >/dev/null 2>&1 || die "python3 is required"
[ -f "$COMPOSE_FILE" ] || die "compose target is missing"

export GRADEX_BACKEND_IMAGE=gradex-verification/backend:local
export GRADEX_PROOF_IMAGE=gradex-verification/proof:local
export GRADEX_FRONTEND_IMAGE=gradex-verification/frontend:local
export STAGING_HOSTNAME=staging.gradex.network
export ACME_EMAIL=acme-verification@example.invalid
export POSTGRES_DB=gradex_playwright_e2e_verify01
export POSTGRES_PASSWORD=verification-only-postgres
export REDIS_PASSWORD=verification-only-redis
export REDIS_TLS_CA_CERT_FILE_HOST=/tmp/gradex-verify/redis-ca.crt
export REDIS_TLS_SERVER_CERT_FILE_HOST=/tmp/gradex-verify/redis-server.crt
export REDIS_TLS_SERVER_KEY_FILE_HOST=/tmp/gradex-verify/redis-server.key
export MINIO_ROOT_USER=verification-root
export MINIO_ROOT_PASSWORD=verification-only-minio-root
export S3_ACCESS_KEY=verification-application
export S3_SECRET_KEY=verification-only-minio-application
export PLAYBACK_TOKEN_SECRET=verification-only-playback
export SESSION_CSRF_KEY=verification-only-session-csrf
export ANONYMOUS_COOKIE_SIGNING_KEY=verification-only-anonymous-cookie
export ANONYMOUS_CSRF_KEY=verification-only-anonymous-csrf
export ADMISSION_LIMITER_HMAC_KEY=verification-only-admission-limiter
export OUTBOX_PROTECTED_PAYLOAD_KEY=verification-only-outbox
export PRIVACY_EMAIL=privacy-verification@example.invalid
export SUPPORT_EMAIL=support-verification@example.invalid
export SECURITY_EMAIL=security-verification@example.invalid

docker compose -f "$COMPOSE_FILE" --profile fixtures config --format json >"$RENDERED"

python3 - "$COMPOSE_FILE" "$RENDERED" <<'PY'
import json
import re
import sys
from pathlib import Path

source_path = Path(sys.argv[1])
model = json.loads(Path(sys.argv[2]).read_text())
source = source_path.read_text()
services = model["services"]


def require(condition: bool, message: str) -> None:
    if not condition:
        raise SystemExit(f"verify-target-compose: {message}")


expected_services = {
    "postgres", "redis", "minio", "minio-init", "migrate",
    "api", "worker", "proof-tool", "frontend", "edge",
}
require(set(services) == expected_services, "service topology changed")

for name in ("migrate", "api", "worker", "proof-tool", "frontend"):
    require(services[name].get("pull_policy") == "never", f"{name} must use pull_policy never")

required_images = {
    "GRADEX_BACKEND_IMAGE": ("migrate", "api", "worker"),
    "GRADEX_PROOF_IMAGE": ("proof-tool",),
    "GRADEX_FRONTEND_IMAGE": ("frontend",),
}
for variable, names in required_images.items():
    require(f"${{{variable}:?" in source, f"{variable} must be required")
    for name in names:
        require(services[name]["image"].startswith("gradex-verification/"), f"{name} image was not interpolated")

backend = services["api"]["environment"]
expected_backend = {
    "APP_ENV": "staging",
    "AUTH_FAKE_MODE": "false",
    "LOGIN_PASSWORD_VERIFY_CONCURRENCY": "1",
    "LOGIN_PASSWORD_VERIFY_QUEUE_CAPACITY": "500",
    "LOGIN_PASSWORD_VERIFY_QUEUE_WAIT": "45s",
    "LOGIN_REQUEST_TIMEOUT": "60s",
    "LEGAL_IDENTITY_MODE": "controlled-staging",
    "LEGAL_REGISTRATION_NUMBER": "STAGING-NOT-REGISTERED",
    "LEGAL_REGISTERED_ADDRESS": "STAGING ONLY — LEGAL ENTITY DETAILS PENDING",
    "EMAIL_ENABLED": "false",
    "STUDENT_REGISTRATION_ENABLED": "false",
    "PASSWORD_SCREEN_MODE": "adapter",
    "COMPROMISED_PASSWORD_ADAPTER_APPROVED": "true",
    "REDIS_TLS_ENABLED": "true",
    "REDIS_TLS_SERVER_NAME": "redis",
    "S3_ENDPOINT": "http://minio:9000",
    "S3_PRESIGN_ENDPOINT": "https://staging.gradex.network",
    "S3_REGION": "auto",
    "S3_USE_PATH_STYLE": "true",
}
for key, expected in expected_backend.items():
    require(str(backend.get(key)).lower() == expected.lower(), f"api {key} is not {expected}")

require(re.fullmatch(r"gradex_playwright_e2e_[A-Za-z0-9_]+", services["postgres"]["environment"]["POSTGRES_DB"]),
        "Postgres database does not match the disposable seed safety pattern")
require("postgres:5432/gradex_playwright_e2e_verify01" in backend["DATABASE_URL"],
        "application database is not the isolated Compose Postgres target")
require(model["volumes"]["postgres-data"] is not None, "dedicated Postgres volume is missing")

redis_command = " ".join(services["redis"]["command"])
require("--port 0" in redis_command and "--tls-port 6379" in redis_command,
        "Redis must expose TLS only")
require("--requirepass" in redis_command, "Redis authentication is missing")
for target in ("/run/gradex/redis/ca.crt", "/run/gradex/redis/server.crt", "/run/gradex/redis/server.key"):
    mounts = [mount for mount in services["redis"]["volumes"] if mount.get("target") == target]
    require(len(mounts) == 1 and mounts[0].get("read_only") is True, f"Redis mount {target} must be read-only")

minio_init = " ".join(services["minio-init"]["command"])
for fragment in ("mc anonymous set none", "mc version enable", "mc admin user add"):
    require(fragment in minio_init, f"MinIO initialization lacks {fragment}")
require("minio-data" in model["volumes"], "disposable MinIO volume is missing")
require(services["minio"]["environment"].get("MINIO_SITE_REGION") == "auto",
        "MinIO region must match the repository storage fixture")
minio_aliases = services["minio"]["networks"]["app"].get("aliases", [])
require("gradex-lg019-media.minio" in minio_aliases,
        "MinIO fixture-compatible private network alias is missing")
require("@storage path /{$$S3_BUCKET}/*" in model["configs"]["caddyfile"]["content"],
        "private path-style MinIO edge route is missing")

for name, service in services.items():
    ports = service.get("ports", [])
    require(name == "edge" or not ports, f"{name} unexpectedly publishes ports")
edge_ports = {
    (str(port["published"]), str(port["target"]), port.get("protocol", "tcp"))
    for port in services["edge"]["ports"]
}
require(edge_ports == {("80", "80", "tcp"), ("443", "443", "tcp"), ("443", "443", "udp")},
        "edge must publish exactly 80/tcp, 443/tcp, and 443/udp")

for name, service in services.items():
    require(not service.get("privileged", False), f"{name} must not be privileged")
    require(service.get("network_mode") != "host", f"{name} must not use host networking")
    require("resources" not in service.get("deploy", {}), f"{name} must not impose artificial resource limits")
    logging = service.get("logging", {})
    require(logging.get("driver") == "json-file", f"{name} must use bounded json-file logging")
    require(logging.get("options") == {"max-file": "5", "max-size": "10m"},
            f"{name} logging bounds changed")
    for mount in service.get("volumes", []):
        require(mount.get("source") != "/var/run/docker.sock" and mount.get("target") != "/var/run/docker.sock",
                f"{name} must not mount the Docker socket")

require(model["networks"]["app"].get("internal") is True, "application network must be internal")
for name in ("postgres", "redis", "minio", "api", "frontend"):
    require("healthcheck" in services[name], f"{name} healthcheck is missing")
require(services["migrate"]["depends_on"]["minio-init"]["condition"] == "service_completed_successfully",
        "migration must wait for MinIO initialization")
for name in ("api", "worker"):
    require(services[name]["depends_on"]["migrate"]["condition"] == "service_completed_successfully",
            f"{name} must wait for migration")
require(services["frontend"]["depends_on"]["api"]["condition"] == "service_healthy",
        "frontend must wait for a healthy API")
for name in ("api", "frontend"):
    require(services["edge"]["depends_on"][name]["condition"] == "service_healthy",
            f"edge must wait for healthy {name}")
require("tls internal" not in model["configs"]["caddyfile"]["content"], "edge must use public ACME TLS")
require("reverse_proxy api:8080" in model["configs"]["caddyfile"]["content"], "API edge route is missing")
require("reverse_proxy frontend:3000" in model["configs"]["caddyfile"]["content"], "frontend edge route is missing")
require(services["proof-tool"].get("profiles") == ["fixtures"], "proof tool must remain opt-in")
require("founder" not in backend["DATABASE_URL"].lower() and "production" not in backend["DATABASE_URL"].lower(),
        "database URL references a founder or production database")

print("verify-target-compose: structural invariants passed")
PY
