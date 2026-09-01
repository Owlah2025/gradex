import assert from "node:assert/strict";
import test from "node:test";
import {
  forgetChallenge,
  recallChallenge,
  rememberChallenge,
  secondsUntil,
} from "./verification-challenge";

type StorageLike = {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
};

function installStorage(storage: StorageLike | (() => never)) {
  const previous = (globalThis as { window?: unknown }).window;
  Object.defineProperty(globalThis, "window", {
    value:
      typeof storage === "function"
        ? {
            get sessionStorage(): never {
              return storage();
            },
          }
        : { sessionStorage: storage },
    configurable: true,
    writable: true,
  });
  return () => {
    Object.defineProperty(globalThis, "window", {
      value: previous,
      configurable: true,
      writable: true,
    });
  };
}

function memoryStorage(): StorageLike & { entries: Map<string, string> } {
  const entries = new Map<string, string>();
  return {
    entries,
    getItem: (key) => entries.get(key) ?? null,
    setItem: (key, value) => {
      entries.set(key, value);
    },
    removeItem: (key) => {
      entries.delete(key);
    },
  };
}

const challenge = {
  challenge_id: "3f1d0a86-6f4a-4a1f-9d0b-2f3a4b5c6d7e",
  masked_email: "ah***@e***.com",
  expires_at: "2026-09-01T12:10:00Z",
  resend_available_at: "2026-09-01T12:01:00Z",
  code_length: 6,
  maximum_attempts: 5,
};

test("a verification challenge survives the navigation to the code screen", () => {
  const restore = installStorage(memoryStorage());
  try {
    rememberChallenge(challenge);
    assert.deepEqual(recallChallenge(challenge.challenge_id), challenge);
    forgetChallenge(challenge.challenge_id);
    assert.equal(recallChallenge(challenge.challenge_id), null);
  } finally {
    restore();
  }
});

test("what comes back out of storage is validated, not trusted", () => {
  const storage = memoryStorage();
  const restore = installStorage(storage);
  try {
    rememberChallenge(challenge);
    const key = [...storage.entries.keys()][0];

    // Storage is shared with everything else on this origin, so a rewritten or
    // truncated entry has to read as absent rather than as a challenge.
    storage.entries.set(key, "not json");
    assert.equal(recallChallenge(challenge.challenge_id), null);

    storage.entries.set(key, JSON.stringify({ challenge_id: challenge.challenge_id }));
    assert.equal(recallChallenge(challenge.challenge_id), null);

    // An entry naming a different challenge must not answer for this one.
    storage.entries.set(
      key,
      JSON.stringify({ ...challenge, challenge_id: "11111111-1111-4111-8111-111111111111" }),
    );
    assert.equal(recallChallenge(challenge.challenge_id), null);
  } finally {
    restore();
  }
});

test("a refused storage does not break the verification screen", () => {
  const restore = installStorage(() => {
    throw new Error("storage is disabled");
  });
  try {
    // Private modes throw on access rather than returning null. None of these
    // may propagate: the screen still works from the identifier in the URL.
    assert.doesNotThrow(() => rememberChallenge(challenge));
    assert.equal(recallChallenge(challenge.challenge_id), null);
    assert.doesNotThrow(() => forgetChallenge(challenge.challenge_id));
  } finally {
    restore();
  }
});

test("the resend countdown is derived from the server's own timestamp", () => {
  const now = Date.parse("2026-09-01T12:00:00Z");
  assert.equal(secondsUntil("2026-09-01T12:01:00Z", now), 60);
  assert.equal(secondsUntil("2026-09-01T12:00:00Z", now), 0);
  // Already past: the control is offered rather than disabled forever.
  assert.equal(secondsUntil("2026-09-01T11:59:00Z", now), 0);
  // An unparseable value shows the control as available. The server still
  // enforces the cooldown, so the worst outcome is one refused request.
  assert.equal(secondsUntil("not a date", now), 0);
});
