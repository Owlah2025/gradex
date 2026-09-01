import assert from "node:assert/strict";
import test from "node:test";
import { parseEmailVerificationAction } from "./e2e-actions";

test("parses an Account-bound email-verification action", () => {
  assert.deepEqual(
    parseEmailVerificationAction(JSON.stringify({
      account_id: "a0000000-0000-0000-0000-000000000111",
      verification_code: "482913",
    })),
    {
      account_id: "a0000000-0000-0000-0000-000000000111",
      verification_code: "482913",
    }
  );
});

test("refuses evidence still shaped like the withdrawn link flow", () => {
  // A payload carrying `verification_token` is either a stale fixture or a
  // seeder that is still reading the link event. Both would leave a spec
  // silently driving a journey the product no longer has.
  assert.throws(
    () =>
      parseEmailVerificationAction(
        JSON.stringify({
          account_id: "a0000000-0000-0000-0000-000000000111",
          verification_token: "opaque-action-secret",
        }),
      ),
    /unusable action/,
  );
});

test("rejects empty, non-JSON, and incomplete email-verification evidence", () => {
  assert.throws(() => parseEmailVerificationAction(""), /did not run/);
  assert.throws(() => parseEmailVerificationAction("not-json"), /non-JSON/);
  assert.throws(
    () => parseEmailVerificationAction(JSON.stringify({ account_id: "account-only" })),
    /unusable action/
  );
});
