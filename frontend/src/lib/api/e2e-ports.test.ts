import assert from "node:assert/strict";
import net from "node:net";
import { test } from "node:test";
import {
  allocateEphemeralPort,
  apiOrigin,
  assertPortIsFree,
  frontendOrigin,
  runPort,
  API_PORT_ENV,
  FRONTEND_PORT_ENV,
} from "./e2e-ports";

function withEnv<T>(keys: string[], body: () => T): T {
  const saved = keys.map((key) => [key, process.env[key]] as const);
  for (const key of keys) delete process.env[key];
  try {
    return body();
  } finally {
    for (const [key, value] of saved) {
      if (value === undefined) delete process.env[key];
      else process.env[key] = value;
    }
  }
}

async function occupy(port: number): Promise<net.Server> {
  const server = net.createServer();
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, "127.0.0.1", resolve);
  });
  return server;
}

test("e2e-ports: allocation never returns a fixed port and never returns 3000", () => {
  const allocated = new Set<number>();
  for (let attempt = 0; attempt < 5; attempt += 1) {
    const port = allocateEphemeralPort();
    assert.ok(Number.isInteger(port) && port > 1024 && port <= 65_535, `unusable port ${port}`);
    assert.notEqual(port, 3000, "the run must never depend on port 3000");
    allocated.add(port);
  }
  assert.ok(allocated.size > 1, "allocation returned the same port every time, which is not dynamic");
});

test("e2e-ports: a worker reuses the run's published ports instead of allocating new ones", () => {
  withEnv([FRONTEND_PORT_ENV, API_PORT_ENV], () => {
    const frontend = runPort(FRONTEND_PORT_ENV);
    const api = runPort(API_PORT_ENV);

    // The environment now carries the decision, exactly as a Playwright worker process sees it.
    assert.equal(process.env[FRONTEND_PORT_ENV], String(frontend));
    assert.equal(runPort(FRONTEND_PORT_ENV), frontend, "a second read must not re-allocate");
    assert.equal(runPort(API_PORT_ENV), api);
    assert.notEqual(frontend, api, "the frontend and API must not be handed the same port");

    assert.equal(frontendOrigin(), `http://127.0.0.1:${frontend}`);
    assert.equal(apiOrigin(), `http://127.0.0.1:${api}`);
  });
});

test("e2e-ports: an unusable published port fails loudly rather than silently re-allocating", () => {
  withEnv([FRONTEND_PORT_ENV], () => {
    process.env[FRONTEND_PORT_ENV] = "not-a-port";
    assert.throws(() => runPort(FRONTEND_PORT_ENV), /unusable port/);
    process.env[FRONTEND_PORT_ENV] = "70000";
    assert.throws(() => runPort(FRONTEND_PORT_ENV), /unusable port/);
  });
});

// The defect this replaces made an E2E run collide with a legitimate developer server on 3000.
test("e2e-ports: a process already listening on 3000 does not affect the run's allocation", async () => {
  let squatter: net.Server | null = null;
  try {
    squatter = await occupy(3000);
  } catch {
    // Port 3000 is already occupied by something else on this machine, which is precisely the
    // condition under test. Either way the assertions below must hold.
  }

  try {
    await withEnv([FRONTEND_PORT_ENV, API_PORT_ENV], async () => {
      const frontend = runPort(FRONTEND_PORT_ENV);
      assert.notEqual(frontend, 3000);
      assert.doesNotMatch(frontendOrigin(), /:3000$/);

      // The run's own port is genuinely free and bindable while 3000 stays occupied.
      await assertPortIsFree(frontend, "The run-owned Next.js server");
      const ours = await occupy(frontend);
      await new Promise<void>((resolve) => ours.close(() => resolve()));
    });
  } finally {
    if (squatter) await new Promise<void>((resolve) => squatter!.close(() => resolve()));
  }
});

test("e2e-ports: an occupied port is refused rather than silently adopted", async () => {
  const port = allocateEphemeralPort();
  const squatter = await occupy(port);
  try {
    await assert.rejects(
      () => assertPortIsFree(port, "The run-owned Next.js server"),
      /cannot bind 127\.0\.0\.1/,
      "an occupied port must fail loudly instead of attaching the suite to another server"
    );
  } finally {
    await new Promise<void>((resolve) => squatter.close(() => resolve()));
  }
  await assertPortIsFree(port, "The run-owned Next.js server");
});
