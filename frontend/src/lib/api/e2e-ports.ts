import { execFileSync } from "child_process";
import net from "net";

/**
 * Per-run port allocation for the E2E infrastructure.
 *
 * Nothing here may depend on a fixed port. A developer server legitimately running on 3000 — in
 * this worktree or any other — is not something an E2E run may collide with, take over, or
 * terminate, so every server this run owns binds a port the kernel hands out for this run alone.
 *
 * Playwright evaluates the config module before anything asynchronous can run, and re-evaluates
 * it inside each worker process. Allocation is therefore synchronous, and the chosen ports are
 * published through the environment so a worker reuses the parent's decision rather than
 * allocating a second, different port.
 */

/**
 * Asks the kernel for a free loopback port by binding port 0 in a short-lived child process.
 * Binding and releasing is the only way to learn a port is genuinely free; the caller binds it
 * for real immediately afterwards.
 */
export function allocateEphemeralPort(): number {
  const script =
    "const net=require('net');const s=net.createServer();" +
    "s.listen(0,'127.0.0.1',()=>{const p=s.address().port;s.close(()=>{process.stdout.write(String(p));});});";
  const output = execFileSync(process.execPath, ["-e", script], { encoding: "utf-8" }).trim();
  const port = Number(output);
  if (!Number.isInteger(port) || port <= 0 || port > 65_535) {
    throw new Error(`Port allocation returned an unusable value: ${output}`);
  }
  return port;
}

/**
 * Returns the run's port for `name`, allocating one the first time and reusing the published
 * value thereafter. `envVar` is run-owned, non-secret state: a port number and nothing else.
 */
export function runPort(envVar: string): number {
  const published = process.env[envVar];
  if (published) {
    const parsed = Number(published);
    if (!Number.isInteger(parsed) || parsed <= 0 || parsed > 65_535) {
      throw new Error(`${envVar} holds an unusable port: ${published}`);
    }
    return parsed;
  }
  const port = allocateEphemeralPort();
  process.env[envVar] = String(port);
  return port;
}

export const FRONTEND_PORT_ENV = "GRADEX_E2E_FRONTEND_PORT";
export const API_PORT_ENV = "GRADEX_E2E_API_PORT";

export function frontendOrigin(): string {
  return `http://127.0.0.1:${runPort(FRONTEND_PORT_ENV)}`;
}

export function apiOrigin(): string {
  return `http://127.0.0.1:${runPort(API_PORT_ENV)}`;
}

/**
 * Confirms the port is free before a server is asked to bind it. Playwright's `webServer` treats
 * an already-serving port as success when reuse is enabled; this run never reuses a server, so an
 * occupied port must fail loudly rather than silently attach the suite to somebody else's server.
 */
export async function assertPortIsFree(port: number, description: string): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    const probe = net.createServer();
    probe.once("error", (error: NodeJS.ErrnoException) => {
      reject(
        new Error(
          `${description} cannot bind 127.0.0.1:${port} (${error.code}). ` +
            `This run allocates its own ports and never takes over an existing server.`
        )
      );
    });
    probe.listen(port, "127.0.0.1", () => probe.close(() => resolve()));
  });
}
