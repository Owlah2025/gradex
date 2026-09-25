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
state_file = fixture / "schema-state"
def schema_state(): return state_file.read_text().strip() if state_file.exists() else c.get("schema_state", "41|false")
if a[0] in ("build", "save", "load"):
    if a[0] != "save": sys.stdin.buffer.read()
    if a[0] == "save": print("mock image archive")
elif a[:2] == ["image", "inspect"]:
    if "--format" in a:
        fmt = a[a.index("--format")+1]
        if "revision" in fmt:
            print(c.get("bad_revision", {}).get(a[-1], c.get("revisions", {}).get(a[-1], c["sha"])))
        else:
            print(c.get("image_id", "sha256:"+hashlib.sha256(a[-1].encode()).hexdigest()))
elif a[0] == "run":
    entry = a[a.index("--entrypoint")+1]
    image = a[a.index("--entrypoint")+2]
    if entry == "sha256sum":
        print(c.get("migration_hash", hashlib.sha256((fixture / "source/backend" / a[-1]).read_bytes()).hexdigest())+"  "+a[-1])
    elif entry == "test":
        # The read-only enhancement drain proof must exist in the same image.
        sys.exit(1 if c.get("no_drain") else 0)
    elif a[-1] == "max-version": print(c.get("ceilings", {}).get(image, c.get("ceiling", "46")))
    elif a[-1] == "schema-range": print(c.get("ranges", {}).get(image, c.get("range", "44 46")))
    else:
        commands = ["up", "down", "version", "max-version"]
        if not c.get("old"): commands.append("rollback-schema-41")
        if not c.get("old") and not c.get("no_schema42_command"): commands.append("rollback-schema-42")
        print("migrate: usage: migrate <"+"|".join(commands)+"> [steps]", file=sys.stderr)
        sys.exit(1)
elif a[0] == "exec":
    command = a[-1]
    if "schema_migrations" in command: print(schema_state())
    elif "work_claim_token" in command: print(c.get("active_claims", "0"))
    elif "source_invitation_id" in command: print("0")
    else: raise RuntimeError(a)
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
        if service in ("postgres", "redis") or (fixture / ("started-"+service)).exists() or service in c.get("present", []): print(worker_id if service == "worker" else service)
    elif "up" in a:
        for service in a[a.index("up")+1:]:
            if not service.startswith("-"): (fixture / ("started-"+service)).touch()
        if "migrate" in a: state_file.write_text(c.get("schema_state_after_up", "46|false"))
    elif "run" in a:
        if "gradex-enhancement-drain" in a:
            if c.get("pending_enhancement"): sys.exit(1)
        elif "rollback-schema-42" in a:
            assert "-confirm-production=schema-42-to-41" in a, a
            if c.get("down_failure"): sys.exit(1)
            state_file.write_text(c.get("schema_state_after_down", "41|false"))
        elif "rollback-schema-41" in a:
            assert "-confirm-production=schema-41-to-40" in a, a
            state_file.write_text("40|false")
        elif "up-schema-45" in a:
            state_file.write_text(c.get("schema_state_after_45", "45|false"))
        elif "up-schema-46" in a:
            state_file.write_text(c.get("schema_state_after_46", "46|false"))
elif a[0] != "info": raise RuntimeError(a)
'''


def run(args, *, env=None, cwd=None, ok=True, input_text=None):
    result = subprocess.run(args, env=env, cwd=cwd, input=input_text, text=True, capture_output=True, timeout=90)
    if ok and result.returncode:
        raise AssertionError(f"{args}: {result.stdout}\n{result.stderr}")
    return result


def manifest(path):
    return dict(line.split("=", 1) for line in path.read_text().splitlines())


# The deployed 3C-A revision, which is the schema-42 release's only application
# rollback target. It is a reviewed constant in release-artifact.sh, so the test
# reads it from there rather than restating it.
FLOOR_SHA = next(line.split("=", 1)[1].strip()
                 for line in (ROOT / "deploy/hostinger/release-artifact.sh").read_text().splitlines()
                 if line.startswith("SCHEMA41_APPLICATION_FLOOR="))


def stage_application_floor(state, tooling, fixture):
    """Stage the 3C-A artifact set exactly as import-release.sh leaves it.

    The floor is a real release that cannot be rebuilt here, so it is
    reconstructed from the candidate's own tooling with the schema-41 boundary
    marker and its own revision. It has to satisfy every check the host applies:
    bundle inventory, extracted-tooling checksums, manifest bindings, image
    identity and the schema-41 version ceiling.
    """
    assert len(FLOOR_SHA) == 40 and all(character in "0123456789abcdef" for character in FLOOR_SHA), FLOOR_SHA
    staging = fixture / "floor-staging"
    shutil.copytree(tooling, staging)
    run(["chmod", "-R", "u+w", str(staging)])
    (staging / "release-tooling.env").write_text(
        f"RELEASE_SHA={FLOOR_SHA}\nDEPLOY_BUNDLE_FORMAT=1\nSCHEMA41_CAPABILITY=supervised-41-to-40-v1\n")
    (staging / "tooling.sha256").unlink()
    run(["bash", "-c", "find . -type f ! -name tooling.sha256 -print0 | sort -z | xargs -0 sha256sum >tooling.sha256"], cwd=staging)

    destination = state / "releases" / FLOOR_SHA
    destination.mkdir(parents=True)
    run(["tar", "-czf", str(destination / "deploy-bundle.tar.gz"), "-C", str(staging), "--transform", "s,^./,,", "."])
    digest = hashlib.sha256((destination / "deploy-bundle.tar.gz").read_bytes()).hexdigest()
    (destination / "deploy-bundle.tar.gz.sha256").write_text(f"{digest}  deploy-bundle.tar.gz\n")
    (destination / "images.tar.gz").write_bytes(b"mock image archive\n")
    (destination / "images.tar.gz.sha256").write_text(
        f"{hashlib.sha256((destination / 'images.tar.gz').read_bytes()).hexdigest()}  images.tar.gz\n")
    (destination / "tooling").mkdir()
    run(["tar", "-xzf", str(destination / "deploy-bundle.tar.gz"), "-C", str(destination / "tooling")])

    images = {role: f"gradex-{'backend-proof' if role == 'PROOF' else role.lower()}:hostinger-{FLOOR_SHA[:12]}"
              for role in ("BACKEND", "FRONTEND", "PROOF")}
    lines = [f"GRADEX_RELEASE_SHA={FLOOR_SHA}"]
    for role, image in images.items():
        lines.append(f"GRADEX_{role}_IMAGE={image}")
    for role, image in images.items():
        lines.append(f"GRADEX_{role}_IMAGE_ID=sha256:{hashlib.sha256(image.encode()).hexdigest()}")
    lines.append(f"GRADEX_DEPLOY_BUNDLE_SHA256={digest}")
    (destination / "release.env").write_text("\n".join(lines) + "\n")
    run(["chmod", "-R", "a-w", str(destination)])
    return {"revisions": {image: FLOOR_SHA for image in images.values()},
            "ceilings": {images["BACKEND"]: "41"}}


def stage_schema46_rollback_artifact(state, tooling, fixture):
    """Stage a distinct old-behaviour artifact for the schema46 boundary."""
    base = "0fee657897c939cb679c9d804d184542bb2f692f"
    patch = hashlib.sha256(b"schema46-rollback-compat").hexdigest()
    release = hashlib.sha256((base + patch).encode()).hexdigest()[:40]
    staging = fixture / "schema46-rollback-staging"
    shutil.copytree(tooling, staging)
    run(["chmod", "-R", "u+w", str(staging)])
    (staging / "release-tooling.env").write_text(
        f"RELEASE_SHA={release}\nDEPLOY_BUNDLE_FORMAT=1\nSCHEMA46_CAPABILITY=auto-enhancement-lesson-preview-v1\n")
    (staging / "tooling.sha256").unlink()
    run(["bash", "-c", "find . -type f ! -name tooling.sha256 -print0 | sort -z | xargs -0 sha256sum >tooling.sha256"], cwd=staging)
    destination = state / "releases" / release
    destination.mkdir(parents=True)
    run(["tar", "-czf", str(destination / "deploy-bundle.tar.gz"), "-C", str(staging), "--transform", "s,^./,,", "."])
    digest = hashlib.sha256((destination / "deploy-bundle.tar.gz").read_bytes()).hexdigest()
    (destination / "deploy-bundle.tar.gz.sha256").write_text(f"{digest}  deploy-bundle.tar.gz\n")
    (destination / "images.tar.gz").write_bytes(b"schema46 rollback images\n")
    (destination / "images.tar.gz.sha256").write_text(f"{hashlib.sha256((destination / 'images.tar.gz').read_bytes()).hexdigest()}  images.tar.gz\n")
    (destination / "tooling").mkdir()
    run(["tar", "-xzf", str(destination / "deploy-bundle.tar.gz"), "-C", str(destination / "tooling")])
    images = {role: f"gradex-{'backend-proof' if role == 'PROOF' else role.lower()}:schema46-rollback-{release[:12]}" for role in ("BACKEND", "FRONTEND", "PROOF")}
    lines = [f"GRADEX_RELEASE_SHA={release}", f"GRADEX_SCHEMA46_ROLLBACK_BASE_SHA={base}", f"GRADEX_SCHEMA46_ROLLBACK_PATCH_SHA256={patch}", "GRADEX_SCHEMA_MIN_VERSION=44", "GRADEX_SCHEMA_MAX_VERSION=46"]
    for role, image in images.items(): lines.append(f"GRADEX_{role}_IMAGE={image}")
    for role, image in images.items(): lines.append(f"GRADEX_{role}_IMAGE_ID=sha256:{hashlib.sha256(image.encode()).hexdigest()}")
    lines.append(f"GRADEX_DEPLOY_BUNDLE_SHA256={digest}")
    (destination / "release.env").write_text("\n".join(lines) + "\n")
    (destination / "schema46-rollback-compat.sha256").write_text(f"{patch}  rollback-compat.patch\n")
    run(["chmod", "-R", "a-w", str(destination)])
    return release, {"revisions": {image: release for image in images.values()}, "ranges": {images["BACKEND"]: "44 46"}, "ceilings": {images["BACKEND"]: "46"}}


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

        # The deployed 3C-A artifact set, staged exactly as import-release.sh
        # leaves it. Without it the schema-42 cutover has no application to roll
        # back to and must refuse, so the forward path cannot be exercised at all
        # until the floor is present.
        floor = stage_application_floor(state, tooling, fixture)
        config = {**config, "revisions": floor["revisions"], "ceilings": floor["ceilings"]}
        rollback_sha, rollback_images = stage_schema46_rollback_artifact(state, tooling, fixture)
        config = {**config, **rollback_images}
        values["GRADEX_SCHEMA46_ROLLBACK_RELEASE_SHA"] = rollback_sha
        backup_dir = state / "backups"
        backup_dir.mkdir(exist_ok=True)
        snapshot = "a" * 64
        (backup_dir / "latest.offsite.snapshot").write_text(snapshot + "\n")
        (backup_dir / "latest.completed-at").write_text(str(int(__import__("time").time())) + "\n")
        (backup_dir / "restored-source").write_text(snapshot + "\n")
        (backup_dir / "restored-schema-state").write_text("44|false\n")

        def host(command, overrides=None, configuration=None):
            for started in fixture.glob("started-*"):
                started.unlink()
            (fixture / "schema-state").unlink(missing_ok=True)
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

        # Schema46 uses two independently immutable artifacts. The release
        # wrapper runs only from extracted candidate tooling and sees no Git.
        schema46 = "up-core-schema-46-media-preview"
        result, calls = host(schema46, configuration={**config, "schema_state": "44|false"})
        assert result.returncode == 0, result.stderr
        runs = [call for call in calls if call[0] == "compose" and "run" in call]
        assert any("up-schema-45" in call for call in runs), runs
        assert any("up-schema-46" in call for call in runs), runs

        result, calls = host("rollback-schema46-application", configuration={**config, "schema_state": "46|false"})
        assert result.returncode == 0, result.stderr
        assert any(call[0] == "compose" and "api" in call and "--force-recreate" in call for call in calls), calls

        # Exact marker, capability, backup/restore identity, auto-off and both
        # compiled ranges are pre-mutation gates. Every failure occurs before a
        # migration container is run.
        for mutation in (
            {"schema_state": "43|false"}, {"schema_state": "44|true"},
            {"schema_state": "45|false"}, {"schema_state": "46|false"},
            {"range": "45 46"}, {"range": "44 45"},
            {"ranges": {next(iter(rollback_images["ranges"])): "43 46"}},
        ):
            result, denied = host(schema46, configuration={**config, **mutation})
            assert result.returncode and not any("up-schema-45" in call for call in denied), (mutation, result.stderr)
        result, denied = host(schema46, overrides={"MEDIA_AUTO_ENHANCEMENT_RECOVERY_ENABLED": "true"}, configuration={**config, "schema_state": "44|false"})
        assert result.returncode and not any("up-schema-45" in call for call in denied), result.stderr
        moved = state / "releases" / (rollback_sha + ".missing")
        (state / "releases" / rollback_sha).rename(moved)
        result, denied = host(schema46, configuration={**config, "schema_state": "44|false"})
        assert result.returncode and not any("up-schema-45" in call for call in denied), result.stderr
        moved.rename(state / "releases" / rollback_sha)

        print("deploy-bundle: schema46 dual-artifact no-Git proof passed")
        return

        forward_command = "up-core-schema-42-enhancement-recovery"
        rollback_command = "rollback-schema-42-enhancement-recovery"
        starting = {forward_command: "41|false", rollback_command: "42|false"}

        def migration_ran(calls):
            return any(a[0] == "compose" and (("up" in a and "migrate" in a) or "rollback-schema-42" in a) for a in calls)

        for command in (forward_command, rollback_command):
            base = {**config, "schema_state": starting[command]}
            result, calls = host(command, configuration=base)
            assert result.returncode == 0, result.stderr
            assert migration_ran(calls), calls
            if command == rollback_command:
                # Exactly the dedicated command with its transition-specific
                # acknowledgement, and nothing that could continue to schema 40.
                runs = [a for a in calls if a[0] == "compose" and "run" in a]
                assert [a[-1] for a in runs] == ["gradex-enhancement-drain", "-confirm-production=schema-42-to-41"], runs
                assert not any("rollback-schema-41" in a or "down" in a for a in runs), runs

            # Identity: a mismatched image, a mixed runtime selection, a missing
            # capability, a wrong ceiling or a drifted migration refuses before
            # anything is started.
            for role in ("BACKEND", "FRONTEND", "PROOF"):
                result, calls = host(command, configuration={**base, "bad_revision": {fields[f"GRADEX_{role}_IMAGE"]: "2" * 40}})
                assert result.returncode and not any(a[0] == "compose" for a in calls), result.stderr
            for overrides in ({"GRADEX_RELEASE_SHA": "2" * 40}, {"GRADEX_BACKEND_IMAGE": "gradex-backend:mixed"}):
                result, calls = host(command, overrides=overrides, configuration=base)
                assert result.returncode and not any(a[0] == "compose" for a in calls), result.stderr
            for mutation in ({"old": True}, {"no_schema42_command": True}, {"no_drain": True},
                             {"ceiling": "41"}, {"ceiling": "43"}, {"image_id": "sha256:wrong"}, {"migration_hash": "0"*64}):
                result, calls = host(command, configuration={**base, **mutation})
                assert result.returncode and not any(a[0] == "compose" for a in calls), (mutation, result.stderr)
            for service in ("api", "worker"):
                result, calls = host(command, configuration={**base, "present": [service]})
                assert result.returncode and not migration_ran(calls), result.stderr

            # Quiescence and schema preconditions sit after PostgreSQL is up, so
            # what they must prevent is the migration, not every Compose call.
            for mutation in ({"active_claims": "2"}, {"schema_state": "40|false"},
                             {"schema_state": starting[command].replace("false", "true")}):
                result, calls = host(command, configuration={**base, **mutation})
                assert result.returncode and not migration_ran(calls), (mutation, result.stderr)

        # The enhancement drain is a hard gate: pending work refuses, and the
        # refusal lands before the schema moves.
        result, calls = host(rollback_command, configuration={**config, "schema_state": "42|false", "pending_enhancement": True})
        assert result.returncode and not any("rollback-schema-42" in a for a in calls), result.stderr

        # A DOWN that fails, or lands anywhere but a clean 41, is not a completed
        # rollback and must not be reported as one.
        for mutation in ({"down_failure": True}, {"schema_state_after_down": "40|false"}, {"schema_state_after_down": "41|true"}):
            result, _ = host(rollback_command, configuration={**config, "schema_state": "42|false", **mutation})
            assert result.returncode, mutation

        # A migration that does not land on a clean 42 must not start the 3C-B
        # application tier: its worker requires 42.
        result, calls = host(forward_command, configuration={**config, "schema_state": "41|false", "schema_state_after_up": "41|false"})
        assert result.returncode and not any(a[0] == "compose" and "up" in a and "api" in a for a in calls), result.stderr

        # No way back means no way forward.
        floor_dir = state / "releases" / FLOOR_SHA
        hidden = state / "releases" / (FLOOR_SHA + ".moved")
        floor_dir.rename(hidden)
        for command in (forward_command, rollback_command):
            result, calls = host(command, configuration={**config, "schema_state": starting[command]})
            assert result.returncode and not any(a[0] == "compose" for a in calls), result.stderr
        hidden.rename(floor_dir)

        # The schema-41 one-release commands stay narrow: this is a schema-42
        # bundle carrying a schema-42 image, and they must refuse both.
        for command in ("up-core-schema-41-foundation", "rollback-schema-41-foundation"):
            result, calls = host(command, configuration={**config, "schema_state": "41|false"})
            assert result.returncode and not any(a[0] == "compose" for a in calls), result.stderr

        # Drift/missing metadata must refuse before even Compose configuration.
        # The capability marker is part of that: a bundle declaring the previous
        # release's boundary, both boundaries, or an unknown one is a mixed or
        # stale bundle and this command must not act on it.
        for path, replacement in ((tooling / "release-tooling.env", None),
                                  (tooling / "release-tooling.env", b"RELEASE_SHA=" + b"2"*40 + b"\n"),
                                  (tooling / "release-tooling.env", f"RELEASE_SHA={sha}\nDEPLOY_BUNDLE_FORMAT=0\n".encode()),
                                  (tooling / "release-tooling.env",
                                   f"RELEASE_SHA={sha}\nDEPLOY_BUNDLE_FORMAT=1\nSCHEMA41_CAPABILITY=supervised-41-to-40-v1\n".encode()),
                                  (tooling / "release-tooling.env",
                                   f"RELEASE_SHA={sha}\nDEPLOY_BUNDLE_FORMAT=1\nSCHEMA41_CAPABILITY=supervised-41-to-40-v1\n"
                                   f"SCHEMA42_CAPABILITY=manual-enhancement-v1\n".encode()),
                                  (tooling / "release-tooling.env",
                                   f"RELEASE_SHA={sha}\nDEPLOY_BUNDLE_FORMAT=1\nSCHEMA42_CAPABILITY=unreviewed\n".encode()),
                                  (tooling / "deploy/hostinger/Caddyfile", b"drift"),
                                  (state / "releases" / sha / "deploy-bundle.tar.gz", b"corrupt")):
            old = path.read_bytes()
            path.parent.chmod(0o700)
            path.chmod(0o600)
            if replacement is None: path.unlink()
            else: path.write_bytes(replacement)
            result, calls = host(rollback_command, configuration={**config, "schema_state": "42|false"})
            assert result.returncode and not any(a[0] == "compose" for a in calls), result.stderr
            path.write_bytes(old)
            path.chmod(0o400)

        for db_name, accepted in (("gradex_production", False), ("founder_beta", True), ("lg019", True)):
            container = {"Config": {"Image": "gradex-backend:other", "Cmd": ["gradex-worker"],
                                    "Env": [f"DATABASE_URL=postgres://user:fixture@other-postgres/{db_name}"],
                                    "Labels": {"com.docker.compose.project": "other", "com.docker.compose.service": "worker"}}}
            result, calls = host(rollback_command, configuration={**config, "schema_state": "42|false", "containers": {"other-worker": container}})
            assert (result.returncode == 0) == accepted, result.stderr
        producer_base = {**config, "schema_state": "42|false"}
        result, _ = host(rollback_command, configuration={**producer_base, "ps_failure": True})
        assert result.returncode
        result, _ = host(rollback_command, configuration={**producer_base, "containers": {"uninspectable": {}}, "inspect_failure": True})
        assert result.returncode
        for malformed in ({"Config": {"Cmd": ["gradex-worker"], "Env": []}},
                          {"Config": {"Cmd": ["gradex-worker"], "Env": ["DATABASE_URL=not-a-url"]}},
                          {"Config": {"Cmd": ["gradex-worker"], "Env": ["DATABASE_URL=postgres://fixture@postgres/other?dbname=gradex_production"]}}):
            result, _ = host(rollback_command, configuration={**producer_base, "containers": {"ambiguous-worker": malformed}})
            assert result.returncode
        old_sha = "a272011620296569f180a02c11c33fbbc8d97c73"
        result, calls = host(forward_command, overrides={"GRADEX_RELEASE_SHA": old_sha}, configuration={"sha": old_sha})
        assert result.returncode and not any(a[0] == "compose" for a in calls)
        # Read-only imported trees need write permission solely for test cleanup.
        for directory, _, files in os.walk(fixture):
            Path(directory).chmod(0o700)
            for file in files: (Path(directory) / file).chmod(0o600)
    print("deploy-bundle: export, import, identity, drift, no-Git CLI, scope and local-producer checks passed")


if __name__ == "__main__":
    main()
