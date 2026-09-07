import { expect, test } from "@playwright/test";
import path from "node:path";
import { openAuthoringSections } from "../authoring-sections";

test("thumbnail failure keeps the saved cover and blocks submission until reconciled", async ({ page }) => {
  const courseID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
  const revisionID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";
  const assetID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc";
  const course = { id: courseID, owner_account_id: "owner", lifecycle: "DRAFT", classification_model: "LEGACY_TAXONOMY",
    editable_revision: { id: revisionID, course_id: courseID, state: "DRAFT", revision_number: 1,
      title_ar: "المنطق الرقمي", title_en: "Digital Logic", description_ar: "الوصف", description_en: "Description",
      major_term_id: "major", subject_term_id: "subject", study_year: "YEAR_1", thumbnail_asset_version_id: assetID,
      sections: [{ id: "section", title_ar: "القسم", title_en: "Section", position: 1,
        lessons: [{ id: "lesson", title_ar: "الدرس", title_en: "Lesson", position: 1, video_asset_version_id: "video", files: [] }] }] } };
  await page.route("**/api/v1/session/bootstrap", (route) => route.fulfill({ json: { csrf_token: "thumbnail-test" } }));
  await page.route("**/api/v1/session", (route) => route.fulfill({ json: { status: "ACTIVE", role: "INSTRUCTOR", csrf_token: "thumbnail-test", display_name: "Instructor", idle_expires_at: new Date(Date.now() + 3600000).toISOString(), absolute_expires_at: new Date(Date.now() + 7200000).toISOString() } }));
  await page.route("**/api/v1/courses", (route) => route.fulfill({ json: [course] }));
  await page.route(`**/api/v1/courses/${courseID}`, (route) => route.fulfill({ json: course }));
  await page.route("**/api/v1/taxonomy/terms**", (route) => route.fulfill({ json: [] }));
  await page.route("**/api/v1/authoring/academic/institutions", (route) => route.fulfill({ json: [] }));
  await page.route(`**/thumbnails/${assetID}/card`, (route) => route.fulfill({ contentType: "image/webp", path: path.resolve(__dirname, "../../../backend/internal/media/testdata/thumbnail.webp") }));
  await page.route("**/api/v1/media/uploads", (route) => route.fulfill({ status: 503, contentType: "application/problem+json", json: { status: 503, code: "DEPENDENCY_UNAVAILABLE", title: "Unavailable", type: "about:blank" } }));
  await page.goto("/en/instructor/courses");
  await openAuthoringSections(page);
  const thumbnail = page.getByTestId("course-thumbnail-authoring");
  await expect(thumbnail.locator("img")).toBeVisible();
  await expect(page.getByTestId("submit-for-review")).toBeEnabled();
  await thumbnail.locator('input[type="file"]').setInputFiles(path.resolve(__dirname, "../../../backend/internal/media/testdata/thumbnail.webp"));
  await expect(thumbnail.getByRole("alert")).toContainText("Could not save the thumbnail");
  await expect(thumbnail.locator("img")).toHaveAttribute("src", new RegExp(assetID));
  await expect(page.getByTestId("submit-for-review")).toBeDisabled();
  await thumbnail.getByRole("button", { name: "Reload saved cover" }).click();
  await expect(thumbnail.getByRole("alert")).toHaveCount(0);
  await expect(page.getByTestId("submit-for-review")).toBeEnabled();
});
