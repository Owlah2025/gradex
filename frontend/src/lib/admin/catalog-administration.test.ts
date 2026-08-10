import { test } from "node:test";
import assert from "node:assert/strict";
import {
  administerCourse,
  administeredCourseID,
  administeredRevisionID,
  closePricingModal,
  initialAdminCatalogState,
  lifecycleControlsVisible,
  openPricingModal,
  pricingModalVisible,
  stopAdministeringCourse,
  taxonomyControlsVisible,
} from "./catalog-administration";

const COURSE_ID = "22f215eb-42fc-4bcd-b01e-37ea967a90b8";
const REVISION_ID = "9c1f0b2a-1111-4a2b-8c3d-4e5f60718293";
const OTHER_COURSE_ID = "3f8b1d40-2222-4c3d-9e1f-8a7b6c5d4e3f";

test("nothing is administered and nothing is open before a Course is chosen", () => {
  assert.equal(administeredCourseID(initialAdminCatalogState), null);
  assert.equal(taxonomyControlsVisible(initialAdminCatalogState), false);
  assert.equal(lifecycleControlsVisible(initialAdminCatalogState), false);
  assert.equal(pricingModalVisible(initialAdminCatalogState), false);
});

test("taxonomy administration survives closing the pricing modal", () => {
  // The founder's exact manual sequence: administer a Course, open pricing,
  // close pricing. Before this fix the third step took taxonomy with it.
  let state = administerCourse(initialAdminCatalogState, { courseID: COURSE_ID, revisionID: REVISION_ID });
  assert.equal(taxonomyControlsVisible(state), true);

  state = openPricingModal(state);
  assert.equal(pricingModalVisible(state), true);
  assert.equal(taxonomyControlsVisible(state), true);

  state = closePricingModal(state);
  assert.equal(pricingModalVisible(state), false, "the pricing dialog must close");
  assert.equal(taxonomyControlsVisible(state), true, "taxonomy administration must survive the close");
  assert.equal(lifecycleControlsVisible(state), true, "lifecycle administration must survive the close");
  assert.equal(administeredCourseID(state), COURSE_ID);
  assert.equal(administeredRevisionID(state), REVISION_ID);
});

test("taxonomy administration does not require the pricing modal to have been opened at all", () => {
  const state = administerCourse(initialAdminCatalogState, { courseID: COURSE_ID, revisionID: null });
  assert.equal(pricingModalVisible(state), false);
  assert.equal(taxonomyControlsVisible(state), true);
  assert.equal(administeredRevisionID(state), null, "an unknown revision is stated, not guessed");
});

test("the pricing modal cannot open against no Course", () => {
  const state = openPricingModal(initialAdminCatalogState);
  assert.equal(pricingModalVisible(state), false);
  assert.equal(administeredCourseID(state), null);
});

test("switching to another Course closes a pricing dialog bound to the previous one", () => {
  let state = administerCourse(initialAdminCatalogState, { courseID: COURSE_ID, revisionID: REVISION_ID });
  state = openPricingModal(state);
  state = administerCourse(state, { courseID: OTHER_COURSE_ID, revisionID: null });

  assert.equal(administeredCourseID(state), OTHER_COURSE_ID);
  assert.equal(pricingModalVisible(state), false, "a dialog bound to the previous Course must not survive");
  assert.equal(taxonomyControlsVisible(state), true);
});

test("re-selecting the same Course leaves an open pricing dialog alone", () => {
  let state = administerCourse(initialAdminCatalogState, { courseID: COURSE_ID, revisionID: REVISION_ID });
  state = openPricingModal(state);
  state = administerCourse(state, { courseID: COURSE_ID, revisionID: REVISION_ID });
  assert.equal(pricingModalVisible(state), true);
});

test("a blank Course identifier administers nothing", () => {
  const state = administerCourse(initialAdminCatalogState, { courseID: "   ", revisionID: null });
  assert.equal(administeredCourseID(state), null);
  assert.equal(taxonomyControlsVisible(state), false);
});

test("ending administration closes everything", () => {
  let state = administerCourse(initialAdminCatalogState, { courseID: COURSE_ID, revisionID: REVISION_ID });
  state = openPricingModal(state);
  state = stopAdministeringCourse(state);
  assert.equal(administeredCourseID(state), null);
  assert.equal(pricingModalVisible(state), false);
  assert.equal(taxonomyControlsVisible(state), false);
});
