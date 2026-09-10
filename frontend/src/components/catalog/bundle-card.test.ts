import assert from "node:assert/strict";
import test from "node:test";
import { bundleCourseCount, bundleDetailHref } from "./bundle-presentation";

test("Bundle navigation and Course counts are localized without changing identity", () => {
  assert.equal(bundleCourseCount(3, "{count} Courses"), "3 Courses");
  assert.equal(bundleCourseCount(3, "{count} كورسات"), "3 كورسات");
  assert.equal(bundleDetailHref("en", "bundle-bundleid"), "/en/catalog/bundles/bundle-bundleid");
  assert.equal(bundleDetailHref("ar", "bundle name"), "/ar/catalog/bundles/bundle%20name");
});
