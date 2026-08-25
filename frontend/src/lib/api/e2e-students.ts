/**
 * Rate-limit-safe Student allocation — the pure arithmetic, kept apart from the Playwright and
 * child-process concerns so the collision properties can be proved directly.
 *
 * Production rate limits are never weakened for a test, so a repeated suite cannot run on one
 * Student: playback issuance is bounded at 30 per 10 minutes per Student, and every execution
 * that authenticates also consumes session resolution. Each execution is therefore handed its own
 * Student, and no two executions in a run share one — which also means no execution inherits the
 * previous repetition's Progress.
 *
 * Allocation is a pure function of the execution's identity, never of execution order:
 *
 *     student index = test slot x MAX_REPEATS + repeat index
 *
 * The slot is fixed where the test is declared and encodes every axis the test is generated
 * across — viewport (project) and, for the Lesson Player matrix, locale. The repeat index
 * separates repetitions. Worker index is deliberately absent: it varies with scheduling, and an
 * allocation that depended on it would not be reproducible.
 */

/** Mirrors `rotatingMaxRepeats` in backend/cmd/e2e-seed/rotating_students_test.go. */
export const ROTATING_MAX_REPEATS = 10;
/** Mirrors `rotatingTestSlots`. */
export const ROTATING_TEST_SLOTS = 35;
/** Mirrors `rotatingStudentPoolSize`. */
export const ROTATING_POOL_SIZE = 350;
/** Mirrors `rotatingExpiredSlots`. */
export const ROTATING_EXPIRED_SLOTS = 8;
/** Mirrors `rotatingExpiredPoolSize`. */
export const ROTATING_EXPIRED_POOL_SIZE = 100;

// The slot map. Each declared test occupies a distinct slot for every axis it is generated
// across, so no two executions in a run resolve to the same Student.
//
//   0-7    Lesson Player            4 viewports x 2 locales
//   8-11   generic unavailable      4 viewports
//   12-15  media & lifecycle        4 viewports
//   16     Progress persistence     1
//   17     infrastructure evidence  1
//   18-21  rendered viewport evid.  4 viewports
//   22-23  Dashboard resume (F15)   English and Arabic, one Student each
//   24-29  Student academic profile 6 T3 journeys, one Student each
//   30-33  Entitlement lifecycle    4 T8A BR-026 operations, one Student each
//
// Growing the map never reassigns an existing execution: allocation is
// slot * repeats + repeat, so slots 0-23 keep indices 0-239 and slots 0-29 keep
// indices 0-299.
//
// Expired pool: 0-3, the expired-access matrix's 4 viewports.

/** The viewport x locale Lesson Player executions occupy active slots 0-7. */
export function playerTestSlot(viewportIndex: number, localeIndex: number, localeCount: number): number {
  return viewportIndex * localeCount + localeIndex;
}
/** The per-viewport generic-unavailable executions occupy active slots 8-11. */
export function genericTestSlot(viewportIndex: number): number {
  return 8 + viewportIndex;
}
/** The per-viewport lifecycle executions occupy active slots 12-15. */
export function lifecycleTestSlot(viewportIndex: number): number {
  return 12 + viewportIndex;
}
/** The per-viewport expired executions occupy expired slots 0-3. */
export function expiredTestSlot(viewportIndex: number): number {
  return viewportIndex;
}
export const PROGRESS_TEST_SLOT = 16;
/**
 * The MVP-F15 Dashboard resume journeys. Each locale gets its own Student so neither test
 * inherits the other's Progress, and neither shares the Lesson Player matrix's playback budget.
 */
export const DASHBOARD_RESUME_EN_TEST_SLOT = 22;
export const DASHBOARD_RESUME_AR_TEST_SLOT = 23;
/** Retained for infrastructure evidence that needs a second, distinct active Student. */
export const LIFECYCLE_TEST_SLOT = 17;

/**
 * The T3 Student academic profile journeys occupy active slots 24-29, one
 * Student each so no journey observes another's onboarding state.
 */
export const ACADEMIC_ONBOARDING_TEST_SLOT = 24;
export const ACADEMIC_DSAI_TEST_SLOT = 25;
export const ACADEMIC_UNDECLARED_TEST_SLOT = 26;
export const ACADEMIC_SKIP_TEST_SLOT = 27;
export const ACADEMIC_INVITATION_TEST_SLOT = 28;
export const ACADEMIC_ACCESS_PRESERVED_TEST_SLOT = 29;

/**
 * The T8A / MVP-F24A Entitlement lifecycle journeys occupy active slots 30-33.
 *
 * Each BR-026 operation gets its own Student because each mutates the Entitlement it acts on:
 * sharing one Student would mean a later case observing an earlier case's expiry or revocation
 * rather than the seeded ACTIVE grant its Admin journey starts from.
 */
export const ENTITLEMENT_EXTEND_TEST_SLOT = 30;
export const ENTITLEMENT_SHORTEN_TEST_SLOT = 31;
export const ENTITLEMENT_PAST_DATE_TEST_SLOT = 32;
export const ENTITLEMENT_REVOKE_TEST_SLOT = 33;
/** AD-14 Student report submission and Admin resolution. */
export const ADMIN_REPORTED_CONTENT_TEST_SLOT = 34;
/**
 * The per-viewport rendered-evidence executions occupy active slots 18-21. Each walks every S5
 * screen in both locales, so it authenticates once and issues at most two playback authorizations
 * — well inside the per-Student budgets — while never sharing a Student with the Lesson Player
 * matrix whose Progress state it would otherwise inherit.
 */
export function viewportEvidenceTestSlot(viewportIndex: number): number {
  return 18 + viewportIndex;
}

export type RotatingStudent = {
  index: number;
  accountID: string;
  email: string;
  access: "active" | "expired";
};

/** The minimum of Playwright's TestInfo this allocation depends on. */
export type AllocationIdentity = { repeatEachIndex: number };

function allocate(
  identity: AllocationIdentity,
  slot: number,
  slots: number,
  poolSize: number,
  access: "active" | "expired"
): RotatingStudent {
  if (!Number.isInteger(slot) || slot < 0 || slot >= slots) {
    throw new Error(`Test slot ${slot} is outside the ${slots} seeded ${access} slots.`);
  }
  if (!Number.isInteger(identity.repeatEachIndex) || identity.repeatEachIndex < 0) {
    throw new Error(`Repeat index ${identity.repeatEachIndex} is not a usable execution identity.`);
  }
  if (identity.repeatEachIndex >= ROTATING_MAX_REPEATS) {
    throw new Error(
      `--repeat-each=${identity.repeatEachIndex + 1} exceeds the ${ROTATING_MAX_REPEATS} repeats the ` +
        `rotating Student pool provisions. Grow the pool rather than reducing repetitions.`
    );
  }
  // No modulo anywhere: allocation must fail rather than wrap, because a wrap silently hands two
  // executions the same Student and reintroduces shared budgets and shared Progress.
  const index = slot * ROTATING_MAX_REPEATS + identity.repeatEachIndex;
  if (index >= poolSize) {
    throw new Error(`Rotating ${access} Student index ${index} exceeds the seeded pool of ${poolSize}.`);
  }
  const prefix = access === "active" ? "a1000000" : "a2000000";
  const emailPart = access === "active" ? "student-rotating" : "student-rotating-expired";
  return {
    index,
    accountID: `${prefix}-0000-0000-0000-${String(index).padStart(12, "0")}`,
    email: `${emailPart}-${String(index).padStart(3, "0")}@example.test`,
    access,
  };
}

/** The active Student this execution owns. */
export function studentFor(identity: AllocationIdentity, slot: number): RotatingStudent {
  return allocate(identity, slot, ROTATING_TEST_SLOTS, ROTATING_POOL_SIZE, "active");
}

/** The expired-access Student this execution owns. */
export function expiredStudentFor(identity: AllocationIdentity, slot: number): RotatingStudent {
  return allocate(identity, slot, ROTATING_EXPIRED_SLOTS, ROTATING_EXPIRED_POOL_SIZE, "expired");
}
