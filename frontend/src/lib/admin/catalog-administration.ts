/**
 * Admin Catalog surface state.
 *
 * The founder's manual acceptance found taxonomy administration disappearing
 * the moment the pricing modal was closed, because one piece of state was
 * carrying two unrelated meanings: *which Course is being administered* and
 * *is the pricing dialog on screen*. They are separated here, and the
 * separation is asserted rather than left to the component tree.
 *
 * This module is deliberately free of React and of network calls so the rule —
 * closing the pricing modal never ends administration — is provable directly.
 */

export type AdministeredCourse = {
  courseID: string;
  /**
   * The revision under administration, when one is known.
   *
   * A Course opened from the review queue always names its submitted revision.
   * A Course administered by UUID has no revision until the administrator
   * names one, and `null` says exactly that instead of guessing.
   */
  revisionID: string | null;
  titleAr?: string;
  titleEn?: string;
};

export type AdminCatalogState = {
  administered: AdministeredCourse | null;
  pricingModalOpen: boolean;
};

export const initialAdminCatalogState: AdminCatalogState = {
  administered: null,
  pricingModalOpen: false,
};

/**
 * Puts one Course under administration.
 *
 * Selecting a different Course closes the pricing modal, because a dialog
 * bound to the previous Course must not survive as if it addressed the new
 * one. Re-selecting the same Course leaves the modal exactly as it was.
 */
export function administerCourse(
  state: AdminCatalogState,
  course: AdministeredCourse,
): AdminCatalogState {
  const courseID = course.courseID.trim();
  if (!courseID) return state;
  const sameCourse = state.administered?.courseID === courseID;
  return {
    administered: { ...course, courseID, revisionID: course.revisionID ?? null },
    pricingModalOpen: sameCourse ? state.pricingModalOpen : false,
  };
}

/** Ends administration entirely: nothing is selected, nothing is open. */
export function stopAdministeringCourse(state: AdminCatalogState): AdminCatalogState {
  if (!state.administered) return state;
  return initialAdminCatalogState;
}

/** Opens pricing for the administered Course. With nothing administered there is nothing to price. */
export function openPricingModal(state: AdminCatalogState): AdminCatalogState {
  if (!state.administered) return state;
  if (state.pricingModalOpen) return state;
  return { ...state, pricingModalOpen: true };
}

/**
 * Closes the pricing modal and nothing else.
 *
 * This is the defect's fix expressed as a function: the administered Course
 * survives, so taxonomy and lifecycle administration stay available.
 */
export function closePricingModal(state: AdminCatalogState): AdminCatalogState {
  if (!state.pricingModalOpen) return state;
  return { ...state, pricingModalOpen: false };
}

export function pricingModalVisible(state: AdminCatalogState): boolean {
  return state.pricingModalOpen && state.administered !== null;
}

/** Taxonomy administration follows the administered Course, never the pricing dialog. */
export function taxonomyControlsVisible(state: AdminCatalogState): boolean {
  return state.administered !== null;
}

/** Lifecycle and emergency controls follow the administered Course, never the pricing dialog. */
export function lifecycleControlsVisible(state: AdminCatalogState): boolean {
  return state.administered !== null;
}

export function administeredCourseID(state: AdminCatalogState): string | null {
  return state.administered?.courseID ?? null;
}

export function administeredRevisionID(state: AdminCatalogState): string | null {
  return state.administered?.revisionID ?? null;
}
