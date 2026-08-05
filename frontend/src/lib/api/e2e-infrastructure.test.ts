import test from "node:test";
import assert from "node:assert/strict";
import fs from "fs";
import path from "path";
import {
  acquireEnvironmentLock,
  releaseEnvironmentLock,
  verifyProcessOwnership,
  getProcessStartTime,
  writeRunState,
  auditStateFileSecrecy,
  RunState,
} from "./e2e-infrastructure.js";

const testDir = "/var/tmp/gradex-e2e-infra-test-" + Date.now();
if (!fs.existsSync(testDir)) {
  fs.mkdirSync(testDir, { recursive: true });
}

const testLockFile = path.join(testDir, "test-environment.lock");
const testStateFile = path.join(testDir, "test-run-state.json");

test("e2e-infrastructure: verifyProcessOwnership checks start identity and cmdline keywords", () => {
  const currentStartTime = getProcessStartTime(process.pid);
  assert.equal(verifyProcessOwnership(process.pid, ["node"], currentStartTime), true);
  // Same PID but different start identity -> false (recycled PID)
  assert.equal(verifyProcessOwnership(process.pid, ["node"], "999999999"), false);
  // Live PID but unrelated keyword -> false
  assert.equal(verifyProcessOwnership(process.pid, ["unrelated_keyword_xyz_999"], currentStartTime), false);
  // Non-existent PID -> false
  assert.equal(verifyProcessOwnership(999999, ["node"]), false);
});

test("e2e-infrastructure: acquireEnvironmentLock and releaseEnvironmentLock cycle", () => {
  const runId1 = "test_run_1";
  const lock1 = acquireEnvironmentLock(runId1, testStateFile, testLockFile);
  assert.equal(lock1.runId, runId1);
  assert.equal(fs.existsSync(testLockFile), true);

  // Attempting to acquire with another runId while process.pid is live MUST fail
  assert.throws(
    () => acquireEnvironmentLock("test_run_2", testStateFile, testLockFile),
    /E2E environment is locked by active process/
  );

  // Attempting to release with another runId MUST return false and preserve lock
  const releasedWrong = releaseEnvironmentLock("test_run_2", testLockFile);
  assert.equal(releasedWrong, false);
  assert.equal(fs.existsSync(testLockFile), true);

  // Release lock with correct runId
  const released = releaseEnvironmentLock(runId1, testLockFile);
  assert.equal(released, true);
  assert.equal(fs.existsSync(testLockFile), false);
});

test("e2e-infrastructure: safe stale-lock and corrupt-lock recovery", () => {
  const staleLock = {
    ownerPid: 999999, // Non-existent PID
    ownerCmd: "node fake.js",
    processStartTime: "12345",
    runId: "stale_run",
    stateFilePath: testStateFile,
    createdAt: new Date().toISOString(),
  };
  fs.writeFileSync(testLockFile, JSON.stringify(staleLock, null, 2));

  // Stale lock recovery must succeed automatically
  const newRunId = "recovery_run";
  const lock = acquireEnvironmentLock(newRunId, testStateFile, testLockFile);
  assert.equal(lock.runId, newRunId);
  releaseEnvironmentLock(newRunId, testLockFile);

  // Corrupt lock file recovery
  fs.writeFileSync(testLockFile, "CORRUPT_JSON{{{");
  const corruptRunId = "corrupt_recovery_run";
  const lockCorrupt = acquireEnvironmentLock(corruptRunId, testStateFile, testLockFile);
  assert.equal(lockCorrupt.runId, corruptRunId);
  releaseEnvironmentLock(corruptRunId, testLockFile);
});

test("e2e-infrastructure: state file secrecy audit", () => {
  const cleanState: RunState = {
    runId: "clean_123",
    pid: 1234,
    port: 8085,
    apiExecPath: "/var/tmp/api",
    apiListenAddr: "127.0.0.1:8085",
    frontendPort: 41234,
    processStartTime: new Date().toISOString(),
    dbName: "gradex_playwright_e2e_123",
    lockOwner: "clean_123",
    startedAt: new Date().toISOString(),
  };

  writeRunState(cleanState, testStateFile);
  const auditClean = auditStateFileSecrecy(testStateFile);
  assert.equal(auditClean.isSecretFree, true);
  assert.equal(auditClean.foundSecrets.length, 0);

  // Pollute state file with secret
  const secretState = {
    ...cleanState,
    password: "SuperSecretPassword123!",
  };
  fs.writeFileSync(testStateFile, JSON.stringify(secretState, null, 2));
  const auditSecret = auditStateFileSecrecy(testStateFile);
  assert.equal(auditSecret.isSecretFree, false);
  assert.ok(auditSecret.foundSecrets.length > 0);

  // Clean up
  try {
    fs.unlinkSync(testStateFile);
  } catch {}
});
