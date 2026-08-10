import test, { beforeEach } from "node:test";
import assert from "node:assert/strict";
import {
  clearSession,
  currentCSRFToken,
  getSessionView,
  resetSessionForTest,
  setSession,
  subscribeToSession,
  type AuthenticatedSession,
} from "./session";

const sample: AuthenticatedSession = {
  status: "AUTHENTICATED",
  role: "STUDENT",
  display_name: "Fahd",
  csrf_token: "memory-only-token",
  idle_expires_at: "2026-08-07T12:00:00Z",
  absolute_expires_at: "2026-08-30T12:00:00Z",
};

beforeEach(() => {
  resetSessionForTest();
});

test("starts signed out", () => {
  assert.equal(getSessionView(), null);
  assert.equal(currentCSRFToken(), null);
});

test("exposes the session without its CSRF token", () => {
  setSession(sample);
  const view = getSessionView();
  assert.ok(view);
  assert.equal(view.role, "STUDENT");
  assert.equal(view.display_name, "Fahd");
  // The rendering boundary must not carry the secret, even structurally.
  assert.equal("csrf_token" in view, false);
  assert.equal(JSON.stringify(view).includes("memory-only-token"), false);
});

test("carries the restriction into the rendering view", () => {
  // The redirect guard reads this from the view, so it has to survive the
  // secret-stripping boundary rather than being dropped with the token.
  setSession({ ...sample, password_change_required: true });
  assert.equal(getSessionView()?.password_change_required, true);
});

test("treats a response without the restriction field as unrestricted", () => {
  // `sample` omits the field entirely. It must normalize to false, not
  // undefined, so no consumer has to distinguish absent from negative.
  setSession(sample);
  assert.equal(getSessionView()?.password_change_required, false);
});

test("hands the CSRF token only to explicit callers", () => {
  setSession(sample);
  assert.equal(currentCSRFToken(), "memory-only-token");
});

test("clearing drops the session and its CSRF token", () => {
  setSession(sample);
  clearSession();
  assert.equal(getSessionView(), null);
  assert.equal(currentCSRFToken(), null);
});

test("notifies subscribers on set and clear, and stops after unsubscribe", () => {
  const seen: Array<string | null> = [];
  const unsubscribe = subscribeToSession((view) =>
    seen.push(view?.display_name ?? null),
  );

  setSession(sample);
  clearSession();
  unsubscribe();
  setSession(sample);

  assert.deepEqual(seen, ["Fahd", null]);
});

test("never touches browser storage", () => {
  // The store must hold the session in memory only. A write to either storage
  // would survive a reload and turn a memory-only secret into a persisted one.
  const writes: string[] = [];
  const recorder = {
    setItem: (key: string) => writes.push(key),
    getItem: () => null,
    removeItem: (key: string) => writes.push(key),
    clear: () => writes.push("clear"),
    key: () => null,
    length: 0,
  };
  const globals = globalThis as Record<string, unknown>;
  globals.localStorage = recorder;
  globals.sessionStorage = recorder;
  globals.document = { cookie: "" };

  try {
    setSession(sample);
    getSessionView();
    currentCSRFToken();
    clearSession();
    assert.deepEqual(writes, []);
    assert.equal((globals.document as { cookie: string }).cookie, "");
  } finally {
    delete globals.localStorage;
    delete globals.sessionStorage;
    delete globals.document;
  }
});
