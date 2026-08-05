import assert from "node:assert/strict";
import test from "node:test";
import fs from "fs";
import { PassThrough } from "stream";
import {
  activeApiLogPath,
  apiLogPath,
  archiveApiLog,
  closeApiLogDrain,
  startApiLogDrain,
  API_LOG_ARCHIVE_DIR,
} from "./e2e-infrastructure.js";

function fakeChild() {
  return { stdout: new PassThrough(), stderr: new PassThrough() };
}

async function settled(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 50));
}

test("api log drain: the run-owned path follows the documented pattern", () => {
  assert.equal(apiLogPath("abc123"), "/var/tmp/gradex-s5-e2e-api-abc123.log");
});

// The defect this guards: a child spawned with piped stdio and no consumer blocks on its own
// write once the pipe buffer fills, wedging the API mid-run. Both pipes must be drained.
test("api log drain: stdout and stderr are both consumed and cannot fill", async () => {
  const runId = `drain-${process.pid}-both`;
  const child = fakeChild();
  const logPath = startApiLogDrain(child, runId);
  try {
    assert.equal(activeApiLogPath(), logPath);

    // Far more than the ~64 KB pipe buffer that wedged the API when nothing was reading.
    const block = "x".repeat(1024);
    for (let written = 0; written < 200; written += 1) child.stdout.write(`${block}\n`);
    for (let written = 0; written < 200; written += 1) child.stderr.write(`${block}\n`);
    await settled();

    // Both streams kept flowing: a blocked pipe would leave them unreadable and paused.
    assert.equal(child.stdout.isPaused(), false, "stdout is not being consumed");
    assert.equal(child.stderr.isPaused(), false, "stderr is not being consumed");
    assert.ok(fs.statSync(logPath).size > 300_000, "drained output did not reach the run-owned file");
  } finally {
    closeApiLogDrain();
    // The stream opens asynchronously, so settle before unlinking or a late open recreates it.
    await settled();
    try { fs.unlinkSync(logPath); } catch {}
  }
});

test("api log drain: closing releases the descriptor and is safe to repeat", async () => {
  const runId = `drain-${process.pid}-close`;
  const child = fakeChild();
  const logPath = startApiLogDrain(child, runId);
  child.stdout.write("first\n");
  await settled();

  closeApiLogDrain();
  await settled();
  assert.equal(activeApiLogPath(), null, "the drain must not outlive the run that opened it");

  // Idempotent: setup-failure cleanup and teardown may both call it.
  assert.doesNotThrow(() => closeApiLogDrain());

  // A child that keeps writing after close cannot resurrect the descriptor.
  assert.doesNotThrow(() => child.stdout.write("after close\n"));
  await settled();
  const afterClose = fs.readFileSync(logPath, "utf-8");
  assert.match(afterClose, /first/);
  assert.doesNotMatch(afterClose, /after close/, "writes after close must not reach the closed log");
  fs.unlinkSync(logPath);
});

test("api log drain: starting a new run closes the previous drain", async () => {
  const firstRun = `drain-${process.pid}-first`;
  const secondRun = `drain-${process.pid}-second`;
  const firstPath = startApiLogDrain(fakeChild(), firstRun);
  const secondPath = startApiLogDrain(fakeChild(), secondRun);
  try {
    assert.notEqual(firstPath, secondPath);
    assert.equal(activeApiLogPath(), secondPath, "only the current run's drain stays active");
  } finally {
    closeApiLogDrain();
    await settled();
    for (const p of [firstPath, secondPath]) { try { fs.unlinkSync(p); } catch {} }
  }
});

test("api log drain: teardown archives the log rather than deleting the evidence", async () => {
  const runId = `drain-${process.pid}-archive`;
  const child = fakeChild();
  const logPath = startApiLogDrain(child, runId);
  child.stdout.write('{"status":200,"limiter_outcome":"ALLOW"}\n');
  await settled();
  closeApiLogDrain();
  await settled();

  const archived = archiveApiLog(runId);
  assert.ok(archived, "the drained log must be retained for limiter evidence");
  assert.ok(archived!.startsWith(API_LOG_ARCHIVE_DIR));
  assert.equal(fs.existsSync(logPath), false, "the run-owned path is released after archiving");
  assert.match(fs.readFileSync(archived!, "utf-8"), /limiter_outcome/);
  fs.unlinkSync(archived!);

  // Archiving a run that produced no log is not an error.
  assert.equal(archiveApiLog(`${runId}-missing`), null);
});
