import type { AllocationIdentity, RotatingStudent } from "./e2e-students";

export const DEVICE_SECURITY_MAX_REPEATS = 10;
export const DEVICE_SECURITY_TEST_SLOTS = 5;
export const DEVICE_SECURITY_POOL_SIZE =
  DEVICE_SECURITY_TEST_SLOTS * DEVICE_SECURITY_MAX_REPEATS;

export const DEVICE_PLAYBACK_TEST_SLOT = 0;
export const DEVICE_MANAGEMENT_TEST_SLOT = 1;
export const DEVICE_LOCALE_TEST_SLOT = 2;
export const DEVICE_SAME_DEVICE_TEST_SLOT = 3;
export const DEVICE_LIMIT_TEST_SLOT = 4;

/** Allocates from the dedicated device-security pool seeded outside Admin queue ordering. */
export function deviceSecurityStudentFor(
  identity: AllocationIdentity,
  slot: number,
): RotatingStudent {
  if (!Number.isInteger(slot) || slot < 0 || slot >= DEVICE_SECURITY_TEST_SLOTS) {
    throw new Error(`Device-security test slot ${slot} is outside the dedicated pool.`);
  }
  if (
    !Number.isInteger(identity.repeatEachIndex) ||
    identity.repeatEachIndex < 0 ||
    identity.repeatEachIndex >= DEVICE_SECURITY_MAX_REPEATS
  ) {
    throw new Error(`Device-security repeat index ${identity.repeatEachIndex} is not provisioned.`);
  }
  const index = slot * DEVICE_SECURITY_MAX_REPEATS + identity.repeatEachIndex;
  if (index >= DEVICE_SECURITY_POOL_SIZE) {
    throw new Error(`Device-security Student index ${index} exceeds the dedicated pool.`);
  }
  return {
    index,
    accountID: `ad000000-0000-0000-0000-${String(index).padStart(12, "0")}`,
    email: `student-device-security-${String(index).padStart(3, "0")}@example.test`,
    access: "active",
  };
}
