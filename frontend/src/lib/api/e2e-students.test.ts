import assert from "node:assert/strict";
import { test } from "node:test";
import {
  expiredStudentFor,
  expiredTestSlot,
  genericTestSlot,
  lifecycleTestSlot,
  playerTestSlot,
  studentFor,
  viewportEvidenceTestSlot,
  PROGRESS_TEST_SLOT,
  ROTATING_EXPIRED_POOL_SIZE,
  ROTATING_EXPIRED_SLOTS,
  ROTATING_MAX_REPEATS,
  ROTATING_POOL_SIZE,
  ROTATING_TEST_SLOTS,
  DASHBOARD_RESUME_AR_TEST_SLOT,
  DASHBOARD_RESUME_EN_TEST_SLOT,
} from "./e2e-students";

const VIEWPORTS = 4;
const LOCALES = 2;

/** Every active-Student execution the T061 matrix generates for one repetition. */
function activeSlotsPerRepeat(): { label: string; slot: number }[] {
  const slots: { label: string; slot: number }[] = [];
  for (let viewport = 0; viewport < VIEWPORTS; viewport += 1) {
    for (let locale = 0; locale < LOCALES; locale += 1) {
      slots.push({ label: `player:v${viewport}:l${locale}`, slot: playerTestSlot(viewport, locale, LOCALES) });
    }
  }
  for (let viewport = 0; viewport < VIEWPORTS; viewport += 1) {
    slots.push({ label: `generic:v${viewport}`, slot: genericTestSlot(viewport) });
    slots.push({ label: `lifecycle:v${viewport}`, slot: lifecycleTestSlot(viewport) });
  }
  slots.push({ label: "progress", slot: PROGRESS_TEST_SLOT });
  slots.push({ label: "dashboard-resume:en", slot: DASHBOARD_RESUME_EN_TEST_SLOT });
  slots.push({ label: "dashboard-resume:ar", slot: DASHBOARD_RESUME_AR_TEST_SLOT });
  for (let viewport = 0; viewport < VIEWPORTS; viewport += 1) {
    slots.push({ label: `evidence:v${viewport}`, slot: viewportEvidenceTestSlot(viewport) });
  }
  return slots;
}

test("rotating pool: the declared sizes cover the matrix at the greatest supported repeat count", () => {
  const active = activeSlotsPerRepeat();
  assert.equal(active.length, 23, "the active matrix generates 23 executions per repetition");
  assert.ok(ROTATING_TEST_SLOTS >= active.length, "declared active slots must cover the matrix");
  assert.ok(
    ROTATING_POOL_SIZE >= ROTATING_TEST_SLOTS * ROTATING_MAX_REPEATS,
    `active pool ${ROTATING_POOL_SIZE} must cover ${ROTATING_TEST_SLOTS} slots x ${ROTATING_MAX_REPEATS} repeats`
  );
  assert.ok(
    ROTATING_EXPIRED_POOL_SIZE >= ROTATING_EXPIRED_SLOTS * ROTATING_MAX_REPEATS,
    "expired pool must cover its slots at the greatest supported repeat count"
  );
});

// The property that makes the whole scheme work: across every execution the suite can generate,
// no two resolve to the same Student. A collision would share a playback budget, a Progress
// budget, and a Progress row.
test("rotating pool: no two executions in a run share a Student", () => {
  const seen = new Map<string, string>();
  for (let repeat = 0; repeat < ROTATING_MAX_REPEATS; repeat += 1) {
    for (const { label, slot } of activeSlotsPerRepeat()) {
      const student = studentFor({ repeatEachIndex: repeat }, slot);
      const key = `active:${student.accountID}`;
      const previous = seen.get(key);
      assert.equal(previous, undefined, `collision: ${label}#${repeat} and ${previous} both got ${key}`);
      seen.set(key, `${label}#${repeat}`);
    }
    for (let viewport = 0; viewport < VIEWPORTS; viewport += 1) {
      const student = expiredStudentFor({ repeatEachIndex: repeat }, expiredTestSlot(viewport));
      const key = `expired:${student.accountID}`;
      assert.equal(seen.get(key), undefined, `expired collision at v${viewport}#${repeat}`);
      seen.set(key, `expired:v${viewport}#${repeat}`);
    }
  }
  assert.equal(seen.size, 23 * ROTATING_MAX_REPEATS + VIEWPORTS * ROTATING_MAX_REPEATS);
});

test("rotating pool: the active and expired pools never overlap", () => {
  const active = new Set<string>();
  for (let repeat = 0; repeat < ROTATING_MAX_REPEATS; repeat += 1) {
    for (const { slot } of activeSlotsPerRepeat()) {
      active.add(studentFor({ repeatEachIndex: repeat }, slot).accountID);
    }
  }
  for (let repeat = 0; repeat < ROTATING_MAX_REPEATS; repeat += 1) {
    for (let viewport = 0; viewport < VIEWPORTS; viewport += 1) {
      const expired = expiredStudentFor({ repeatEachIndex: repeat }, expiredTestSlot(viewport));
      assert.equal(active.has(expired.accountID), false, "an expired fixture must never be an active one");
      assert.match(expired.accountID, /^a2000000-/);
      assert.match(expired.email, /^student-rotating-expired-/);
      assert.equal(expired.access, "expired");
    }
  }
});

test("rotating pool: allocation depends only on slot and repeat, never on execution order", () => {
  const first = studentFor({ repeatEachIndex: 3 }, lifecycleTestSlot(2));
  // Same identity resolved again after unrelated allocations still yields the same Student.
  studentFor({ repeatEachIndex: 0 }, PROGRESS_TEST_SLOT);
  expiredStudentFor({ repeatEachIndex: 9 }, expiredTestSlot(1));
  const again = studentFor({ repeatEachIndex: 3 }, lifecycleTestSlot(2));
  assert.deepEqual(again, first);
  assert.equal(first.index, lifecycleTestSlot(2) * ROTATING_MAX_REPEATS + 3);
});

test("rotating pool: allocation fails clearly instead of wrapping", () => {
  // Out of range slot.
  // Derived from the declared constant, so growing the pool cannot leave this assertion behind.
  const outsideSlots = new RegExp(`outside the ${ROTATING_TEST_SLOTS} seeded active slots`);
  assert.throws(() => studentFor({ repeatEachIndex: 0 }, ROTATING_TEST_SLOTS), outsideSlots);
  assert.throws(() => studentFor({ repeatEachIndex: 0 }, -1), outsideSlots);
  assert.throws(() => expiredStudentFor({ repeatEachIndex: 0 }, ROTATING_EXPIRED_SLOTS), /seeded expired slots/);

  // Beyond the provisioned repeat count: grow the pool, never wrap onto an in-use Student.
  assert.throws(
    () => studentFor({ repeatEachIndex: ROTATING_MAX_REPEATS }, 0),
    /exceeds the 10 repeats the rotating Student pool provisions/
  );
  assert.throws(() => studentFor({ repeatEachIndex: -1 }, 0), /not a usable execution identity/);

  // The highest legal allocation still sits inside the seeded pool.
  const highest = studentFor({ repeatEachIndex: ROTATING_MAX_REPEATS - 1 }, ROTATING_TEST_SLOTS - 1);
  assert.equal(highest.index, ROTATING_TEST_SLOTS * ROTATING_MAX_REPEATS - 1);
  assert.ok(highest.index < ROTATING_POOL_SIZE);
});

test("rotating pool: identifiers and emails match the seeded fixtures", () => {
  const student = studentFor({ repeatEachIndex: 4 }, PROGRESS_TEST_SLOT);
  assert.equal(student.index, 164);
  assert.equal(student.accountID, "a1000000-0000-0000-0000-000000000164");
  assert.equal(student.email, "student-rotating-164@example.test");
  assert.equal(student.access, "active");
});
