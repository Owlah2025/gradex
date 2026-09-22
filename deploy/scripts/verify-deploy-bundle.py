#!/usr/bin/env python3
"""Release export/import and real host CLI in an isolated, no-Git host filesystem.

Docker is the only external service mocked. bwrap gives the host its real
production paths without accessing or changing any existing host state.
"""
import hashlib
import json
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[2]
HOST_STATE = "/home/deploy/gradex-production"
MOCK_DOCKER = r'''#!/usr/bin/python3
import hashlib, json, os, sys
from pathlib import Path
a = sys.argv[1:]
fixture = Path(os.environ["FIXTURE"])
c = json.loads((fixture / "docker.json").read_text())
worker_id = "a" * 64
with (fixture / "docker.log").open("a") as f: f.write(json.dumps(a) + "\n")
if a[0] in ("build", "save", "load"):
    if a[0] != "save": sys.stdin.buffer.read()
    if a[0] == "save": print("mock image archive")
elif a[:2] == ["image", "inspect"]:
    if "--format" in a:
        fmt = a[a.index("--format")+1]
        print(c.get("bad_revision", {}).get(a[-1], c["sha"]) if "revision" in fmt else c.get("image_id", "sha256:"+hashlib.sha256(a[-1].encode()).hexdigest()))
elif a[0] == "run":
    entry = a[a.index("--entrypoint")+1]
    if entry == "sha256sum":
        print(c.get("migration_hash", hashlib.sha256((fixture / "source/backend" / a[-1]).read_bytes()).hexdigest())+"  "+a[-1])
    elif a[-1] == "max-version": print(c.get("ceiling", "41"))
    else:
        print("migrate: usage: migrate <up|down|version|max-version"+("" if c.get("old") else "|rollback-schema-41")+"> [steps]", file=sys.stderr)
        sys.exit(1)
elif a[0] == "ps":
    assert "--no-trunc" in a
    if c.get("ps_failure"): sys.exit(1)
    print("\n".join(c.get("containers", {}).keys()))
    if (fixture / "started-worker").exists(): print(worker_id)
elif a[0] == "inspect":
    if "--format" not in a:
        if c.get("inspect_failure"): sys.exit(1)
        if a[-1] == worker_id:
            print(json.dumps([{"Config": {"Cmd": ["gradex-worker"], "Env": ["DATABASE_URL=postgres://fixture@postgres/gradex_production"]}}]))
        else: print(json.dumps([c["containers"][a[-1]]]))
    elif "ExitCode" in a[a.index("--format")+1]: print("0")
    else: print("exited" if a[-1] == "migrate" else "running" if a[-1] == worker_id else "healthy")
elif a[0] == "compose":
    sys.stdin.read()
    assert a[a.index("--project-name")+1] == "gradex-production", a
    if "ps" in a:
        service = a[-1]
        if service == "postgres" or (fixture / ("started-"+service)).exists() or service in c.get("present", []): print(worker_id if service == "worker" else service)
    elif "up" in a:
        for service in a[a.index("up")+1:]:
            if not service.startswith("-"): (fixture / ("started-"+service)).touch()
elif a[0] != "info": raise RuntimeError(a)
'''


def run(args, *, env=None, cwd=None, ok=True, input_text=None):
    result = subprocess.run(args, env=env, cwd=cwd, input=input_text, text=True, capture_output=True, timeout=90)
    if ok and result.returncode:
        raise AssertionError(f"{args}: {result.stdout}\n{result.stderr}")
    return result


def manifest(path):
    return dict(line.split("=", 1) for line in path.read_text().splitlines())


def main():
    if not shutil.which("bwrap"):
        raise SystemExit("bwrap is required for the isolated no-Git host test (workstation only)")
    with tempfile.TemporaryDirectory(prefix="gradex-bundle-") as temporary:
        fixture = Path(temporary)
        source = fixture / "source"
        source.mkdir()
        # A disposable clean builder commit includes the changes under test.
        for directory in ("deploy/hostinger", "deploy/compose", "deploy/monitoring", "deploy/scripts", "backend/internal/db/migrations"):
            shutil.copytree(ROOT / directory, source / directory)
        (source / "frontend").mkdir()
        (source / "frontend/Dockerfile").write_text("FROM scratch\n")
        (source / ".gitignore").write_text("deploy/.state/\n")
        run(["git", "init", "-q", str(source)])
        run(["git", "add", "."], cwd=source)
        run(["git", "-c", "user.name=Bundle Test", "-c", "user.email=bundle@example.test", "commit", "-qm", "fixture"], cwd=source)
        sha = run(["git", "rev-parse", "HEAD"], cwd=source).stdout.strip()
        bins = fixture / "bin"
        bins.mkdir()
        (bins / "docker").write_text(MOCK_DOCKER)
        (bins / "docker").chmod(0o755)
        config = {"sha": sha}
        (fixture / "docker.json").write_text(json.dumps(config))
        env = dict(os.environ, FIXTURE=str(fixture), PATH=f"{bins}:{os.environ['PATH']}")
        release_script = source / "deploy/hostinger/release.sh"
        run(["bash", str(release_script), "build"], env=env)
        run(["bash", str(release_script), "export", sha], env=env)
        release = source / "deploy/.state/hostinger/releases" / sha
        fields = manifest(release / "release.env")
        with tarfile.open(release / "deploy-bundle.tar.gz") as bundle:
            assert not any(".git" in Path(n).parts for n in bundle.getnames())
            assert f"RELEASE_SHA={sha}" in bundle.extractfile("release-tooling.env").read().decode()
        run(["sha256sum", "--check", "deploy-bundle.tar.gz.sha256"], cwd=release)
        (source / "dirty").touch()
        for command in ("build", "record", "export"):
            args = ["bash", str(release_script), command] + ([] if command == "build" else [sha])
            assert run(args, env=env, ok=False).returncode, f"dirty {command} accepted"
        (source / "dirty").unlink()

        state = fixture / "gradex-production"
        incoming = state / "incoming" / sha
        shutil.copytree(release, incoming)
        host_env = dict(env, GRADEX_HOST_STATE_DIR=str(state))
        importer = source / "deploy/hostinger/import-release.sh"
        archive = incoming / "deploy-bundle.tar.gz"
        original = archive.read_bytes()
        archive.write_bytes(original + b"corrupt")
        assert run(["bash", str(importer), sha], env=host_env, ok=False).returncode
        assert not (state / "releases" / sha).exists()
        archive.write_bytes(original)
        incoming_manifest = incoming / "release.env"
        original_manifest = incoming_manifest.read_text()
        for old, new in ((sha, "2"*40), (fields["GRADEX_DEPLOY_BUNDLE_SHA256"], "0"*64)):
            incoming_manifest.write_text(original_manifest.replace(old, new))
            (fixture / "docker.log").write_text("")
            rejected = run(["bash", str(importer), sha], env=host_env, ok=False)
            assert rejected.returncode and not (state / "releases" / sha).exists()
            assert '"load"' not in (fixture / "docker.log").read_text()
        incoming_manifest.write_text(original_manifest)
        run(["bash", str(importer), sha], env=host_env)
        assert run(["bash", str(importer), sha], env=host_env, ok=False).returncode, "immutable import overwritten"
        tooling = state / "releases" / sha / "tooling"
        assert not (tooling / ".git").exists()
        assert os.access(tooling / "deploy/hostinger/host.sh", os.X_OK)
        assert all(path.stat().st_mode & 0o222 == 0 for path in (state / "releases" / sha).rglob("*"))
        # The host cannot execute Git; a call is recorded even if a wrapper ignores failure.
        (bins / "git").write_text('#!/bin/sh\ntouch /tmp/fixture/GIT_CALLED\nexit 99\n')
        (bins / "git").chmod(0o755)
        for name in ("ca.crt", "server.crt", "server.key"):
            path = state / "redis-tls" / name
            path.parent.mkdir(exist_ok=True)
            path.write_text("fixture\n")
        (state / "backup-password").write_text("fixture")
        (state / "backup-password").chmod(0o600)
        values = {}
        for line in (ROOT / "deploy/hostinger/runtime.env.example").read_text().splitlines():
            if line and not line.startswith("#") and "=" in line:
                key, value = line.split("=", 1)
                values[key] = value or "placeholder"
        values.update({key: fields[key] for key in ("GRADEX_RELEASE_SHA", "GRADEX_BACKEND_IMAGE", "GRADEX_FRONTEND_IMAGE", "GRADEX_PROOF_IMAGE")})
        values.update(APP_ENV="production", COMPROMISED_PASSWORD_ADAPTER_APPROVED="true", POSTGRES_DB="gradex_production",
                      DATABASE_URL="postgres://gradex:fixture@postgres:5432/gradex_production?sslmode=disable",
                      PUBLIC_ORIGIN="https://gradex.test", STAGING_HOSTNAME="gradex.test", S3_ENDPOINT="https://example.r2.cloudflarestorage.com",
                      SALES_WHATSAPP_NUMBER="96500000000", GRADEX_BACKUP_S3_ENDPOINT="https://example.test",
                      GRADEX_BACKUP_S3_BUCKET="gradex-backups", GRADEX_BACKUP_PASSWORD_FILE=f"{HOST_STATE}/backup-password",
                      GRADEX_BACKUP_RESTIC_BINARY="/bin/true")
        for key, name in (("CA_CERT", "ca.crt"), ("SERVER_CERT", "server.crt"), ("SERVER_KEY", "server.key")):
            values[f"REDIS_TLS_{key}_FILE_HOST"] = f"{HOST_STATE}/redis-tls/{name}"

        rendered = []
        for tree in (ROOT, tooling):
            compose_dir = tree / "deploy/hostinger"
            rendered.append(json.loads(run([shutil.which("docker"), "compose", "--file", "-",
                                           "--project-directory", str(compose_dir), "--project-name", "gradex-production",
                                           "config", "--format", "json"], env={**os.environ, **values},
                                           input_text=(compose_dir / "compose.yml").read_text()).stdout))
        assert rendered[0]["volumes"] == rendered[1]["volumes"]
        assert rendered[1]["name"] == "gradex-production"
        caddy = rendered[1]["services"]["edge"]["volumes"][0]
        assert caddy["source"] == str(tooling / "deploy/hostinger/Caddyfile")
        assert Path(caddy["source"]).is_file()

        def host(command, overrides=None, configuration=None):
            for started in fixture.glob("started-*"):
                started.unlink()
            (fixture / "docker.log").write_text("")
            (fixture / "docker.json").write_text(json.dumps(configuration or config))
            runtime = dict(values, **(overrides or {}))
            (state / "runtime.env").write_text("".join(f"{k}={shlex.quote(v)}\n" for k, v in runtime.items()))
            (state / "runtime.env").chmod(0o600)
            result = run(["bwrap", "--ro-bind", "/", "/", "--dev", "/dev", "--tmpfs", "/home", "--dir", "/home/deploy",
                          "--bind", str(state), HOST_STATE, "--tmpfs", "/tmp", "--bind", str(fixture), "/tmp/fixture",
                          "--chdir", "/tmp/fixture", "--setenv", "FIXTURE", "/tmp/fixture", "--setenv", "PATH", "/tmp/fixture/bin:/usr/bin:/bin",
                          "--setenv", "GRADEX_HOST_STATE_DIR", HOST_STATE, "--setenv", "GRADEX_HOST_PROJECT", "gradex-production",
                          "bash", f"{HOST_STATE}/releases/{sha}/tooling/deploy/hostinger/host.sh", command], ok=False)
            assert not (fixture / "GIT_CALLED").exists(), "production wrapper called Git"
            calls = [json.loads(line) for line in (fixture / "docker.log").read_text().splitlines()]
            return result, calls

        for command in ("up-core-schema-41-foundation", "rollback-schema-41-foundation"):
            result, calls = host(command)
            assert result.returncode == 0, result.stderr
            assert any(a[0] == "compose" and ("up" in a if command.startswith("up") else "rollback-schema-41" in a) for a in calls)
            for role in ("BACKEND", "FRONTEND", "PROOF"):
                result, calls = host(command, configuration={**config, "bad_revision": {fields[f"GRADEX_{role}_IMAGE"]: "2" * 40}})
                assert result.returncode and not any(a[0] == "compose" for a in calls), result.stderr
            for overrides in ({"GRADEX_RELEASE_SHA": "2" * 40}, {"GRADEX_BACKEND_IMAGE": "gradex-backend:mixed"}):
                result, calls = host(command, overrides=overrides)
                assert result.returncode and not any(a[0] == "compose" for a in calls), result.stderr
            for mutation in ({"old": True}, {"ceiling": "40"}, {"image_id": "sha256:wrong"}, {"migration_hash": "0"*64}):
                result, calls = host(command, configuration={**config, **mutation})
                assert result.returncode and not any(a[0] == "compose" for a in calls), result.stderr
            for service in ("api", "worker"):
                result, calls = host(command, configuration={**config, "present": [service]})
                assert result.returncode and not any(a[0] == "compose" and ("up" in a or "run" in a) for a in calls)

        # Drift/missing metadata must refuse before even Compose configuration.
        for path, replacement in ((tooling / "release-tooling.env", None),
                                  (tooling / "release-tooling.env", b"RELEASE_SHA=" + b"2"*40 + b"\n"),
                                  (tooling / "release-tooling.env", f"RELEASE_SHA={sha}\nDEPLOY_BUNDLE_FORMAT=0\n".encode()),
                                  (tooling / "deploy/hostinger/Caddyfile", b"drift"),
                                  (state / "releases" / sha / "deploy-bundle.tar.gz", b"corrupt")):
            old = path.read_bytes()
            path.parent.chmod(0o700)
            path.chmod(0o600)
            if replacement is None: path.unlink()
            else: path.write_bytes(replacement)
            result, calls = host("rollback-schema-41-foundation")
            assert result.returncode and not any(a[0] == "compose" for a in calls), result.stderr
            path.write_bytes(old)
            path.chmod(0o400)

        for db_name, accepted in (("gradex_production", False), ("founder_beta", True), ("lg019", True)):
            container = {"Config": {"Image": "gradex-backend:other", "Cmd": ["gradex-worker"],
                                    "Env": [f"DATABASE_URL=postgres://user:fixture@other-postgres/{db_name}"],
                                    "Labels": {"com.docker.compose.project": "other", "com.docker.compose.service": "worker"}}}
            result, calls = host("rollback-schema-41-foundation", configuration={**config, "containers": {"other-worker": container}})
            assert (result.returncode == 0) == accepted, result.stderr
        result, _ = host("rollback-schema-41-foundation", configuration={**config, "ps_failure": True})
        assert result.returncode
        result, _ = host("rollback-schema-41-foundation", configuration={**config, "containers": {"uninspectable": {}}, "inspect_failure": True})
        assert result.returncode
        for malformed in ({"Config": {"Cmd": ["gradex-worker"], "Env": []}},
                          {"Config": {"Cmd": ["gradex-worker"], "Env": ["DATABASE_URL=not-a-url"]}},
                          {"Config": {"Cmd": ["gradex-worker"], "Env": ["DATABASE_URL=postgres://fixture@postgres/other?dbname=gradex_production"]}}):
            result, _ = host("rollback-schema-41-foundation", configuration={**config, "containers": {"ambiguous-worker": malformed}})
            assert result.returncode
        old_sha = "a272011620296569f180a02c11c33fbbc8d97c73"
        result, calls = host("up-core-schema-41-foundation", overrides={"GRADEX_RELEASE_SHA": old_sha}, configuration={"sha": old_sha})
        assert result.returncode and not any(a[0] == "compose" for a in calls)
        # Read-only imported trees need write permission solely for test cleanup.
        for directory, _, files in os.walk(fixture):
            Path(directory).chmod(0o700)
            for file in files: (Path(directory) / file).chmod(0o600)
    print("deploy-bundle: export, import, identity, drift, no-Git CLI, scope and local-producer checks passed")


if __name__ == "__main__":
    main()
