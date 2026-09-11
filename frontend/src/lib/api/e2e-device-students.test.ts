import assert from "node:assert/strict";
import test from "node:test";
import {
  DEVICE_SECURITY_TEST_SLOTS,
  deviceSecurityStudentFor,
} from "./e2e-device-students";

test("device-security allocation is isolated and collision-free", () => {
  const identities = new Set<string>();
  for (let slot = 0; slot < DEVICE_SECURITY_TEST_SLOTS; slot += 1) {
    for (let repeatEachIndex = 0; repeatEachIndex < 10; repeatEachIndex += 1) {
      const student = deviceSecurityStudentFor({ repeatEachIndex }, slot);
      assert.match(student.email, /^student-device-security-/);
      assert.equal(identities.has(student.accountID), false);
      identities.add(student.accountID);
    }
  }
  assert.equal(identities.size, 50);
});
