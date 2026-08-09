import test from "node:test";
import assert from "node:assert/strict";
import {
  captureTokenFromFragment,
  isFragmentTokenSpent,
  releaseFragmentToken,
} from "./validation";

/**
 * These tests exercise the module-scoped fragment-token slots.
 *
 * The helpers read `window` when called rather than at import time, so a
 * minimal stub is installed per scenario. Only the members the helpers touch
 * are provided — anything more would make the stub a fiction that passes for
 * reasons the real browser would not.
 */
function installWindow(hash: string) {
  (globalThis as unknown as { window: unknown }).window = {
    location: { hash, pathname: "/recover/reset", search: "" },
    history: { state: null, replaceState() {} },
  };
}

test("capture is namespaced per purpose, not shared across flows", () => {
  installWindow("#token=verification-bearer");
  assert.equal(
    captureTokenFromFragment("EMAIL_VERIFICATION"),
    "verification-bearer",
  );

  // A client-side navigation to recovery leaves the document intact, so module
  // memory survives and the fragment is already gone. Recovery must not
  // inherit the verification bearer: it has its own slot.
  installWindow("");
  assert.equal(captureTokenFromFragment("PASSWORD_RESET"), null);

  // The verification slot is untouched by the recovery capture.
  assert.equal(
    captureTokenFromFragment("EMAIL_VERIFICATION"),
    "verification-bearer",
  );
});

test("capture is monotonic once the fragment has been scrubbed", () => {
  // Scrubbing empties the fragment. A later capture for the same purpose must
  // still return the token, otherwise a successful scrub reads as a missing
  // link — the defect that made the reset form vanish.
  installWindow("");
  assert.equal(
    captureTokenFromFragment("EMAIL_VERIFICATION"),
    "verification-bearer",
  );
});

test("release drops the raw bearer and marks that purpose settled", () => {
  assert.equal(isFragmentTokenSpent("EMAIL_VERIFICATION"), false);
  releaseFragmentToken("EMAIL_VERIFICATION");
  assert.equal(isFragmentTokenSpent("EMAIL_VERIFICATION"), true);
  assert.equal(captureTokenFromFragment("EMAIL_VERIFICATION"), null);
});

test("releasing one purpose does not settle or clear the other", () => {
  // PASSWORD_RESET captured null earlier and was never released, so releasing
  // EMAIL_VERIFICATION must not have settled it.
  assert.equal(isFragmentTokenSpent("PASSWORD_RESET"), false);
});

test("a newly navigated link replaces a settled bearer for the same purpose", () => {
  installWindow("#token=expired-course-invitation");
  assert.equal(
    captureTokenFromFragment("COURSE_ACCESS_INVITATION"),
    "expired-course-invitation",
  );
  releaseFragmentToken("COURSE_ACCESS_INVITATION");

  installWindow("#token=fresh-course-invitation");
  assert.equal(
    captureTokenFromFragment("COURSE_ACCESS_INVITATION"),
    "fresh-course-invitation",
  );
  assert.equal(isFragmentTokenSpent("COURSE_ACCESS_INVITATION"), false);
});
