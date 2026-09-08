import { expect, test, type Page } from "@playwright/test";

const COURSE_ID = "a1000000-0000-4000-8000-000000000001";
const REVISION_ID = "a2000000-0000-4000-8000-000000000002";
const INSTITUTION_ID = "a3000000-0000-4000-8000-000000000003";
const SUBJECT_ID = "a4000000-0000-4000-8000-000000000004";
const sectionIDs = ["b1000000-0000-4000-8000-000000000001", "b2000000-0000-4000-8000-000000000002", "b3000000-0000-4000-8000-000000000003"];
const lessonIDs = ["c1000000-0000-4000-8000-000000000001", "c2000000-0000-4000-8000-000000000002", "c3000000-0000-4000-8000-000000000003"];

type StudioState = ReturnType<typeof studioCourse>;

function studioCourse(state = "DRAFT") {
  return {
    id: COURSE_ID, lifecycle: "PUBLISHED", live_revision_id: "live-revision",
    classification_model: "ACADEMIC_CATALOG", institution_id: INSTITUTION_ID, subject_id: SUBJECT_ID,
    academic_context: { institution_name_ar: "جامعة الكويت", institution_name_en: "Kuwait University", subject: { title_ar: "هندسة", title_en: "Engineering" } },
    editable_revision: {
      id: REVISION_ID, course_id: COURSE_ID, state, revision_number: 2,
      title_ar: "مقرر الأنظمة", title_en: "Systems course",
      description_ar: "وصف المقرر", description_en: "Course description",
      sections: sectionIDs.map((id, sectionIndex) => ({
        id,
        position: sectionIndex,
        title_ar: sectionIndex === 1 ? "قسم طويل العنوان لاختبار التفاف النص على الشاشات الصغيرة" : `القسم ${sectionIndex + 1}`,
        title_en: sectionIndex === 1 ? "A deliberately long section title for narrow authoring screens" : `Section ${sectionIndex + 1}`,
        lessons: sectionIndex === 0 ? lessonIDs.map((lessonID, lessonIndex) => ({
          id: lessonID, position: lessonIndex, title_ar: `الدرس ${lessonIndex + 1}`, title_en: `Lesson ${lessonIndex + 1}`, files: [],
        })) : [],
      })),
    },
  };
}

async function serveStudio(page: Page, state: StudioState, failOrder: () => boolean = () => false) {
  await page.route("**/api/v1/session/bootstrap", (route) => route.fulfill({ json: { csrf_token: "csrf" } }));
  await page.route("**/api/v1/session", (route) => route.fulfill({ json: {
    status: "ACTIVE", role: "INSTRUCTOR", display_name: "Instructor", csrf_token: "csrf",
    idle_expires_at: new Date(Date.now() + 3_600_000).toISOString(),
    absolute_expires_at: new Date(Date.now() + 7_200_000).toISOString(),
  } }));
  await page.route("**/api/v1/authoring/academic/institutions", (route) => route.fulfill({ json: [{ id: INSTITUTION_ID, name_ar: "جامعة الكويت", name_en: "Kuwait University" }] }));
  await page.route("**/api/v1/authoring/academic/institutions/*/subjects/*", (route) => route.fulfill({ json: { id: SUBJECT_ID, title_ar: "هندسة", title_en: "Engineering", programs: [] } }));
  await page.route("**/api/v1/authoring/academic/subject-requests**", (route) => route.fulfill({ json: [] }));
  await page.route("**/api/v1/taxonomy/terms**", (route) => route.fulfill({ json: [] }));
  await page.route("**/api/v1/courses**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (request.method() === "PATCH" && url.pathname.endsWith("/sections/order")) {
      if (failOrder()) {
        await new Promise((resolve) => setTimeout(resolve, 1_500));
        return route.fulfill({ status: 422, contentType: "application/problem+json", body: JSON.stringify({ type: "about:blank", title: "Validation failed", status: 422, code: "VALIDATION_FAILED", detail: "Order was rejected" }) });
      }
      const { section_ids } = request.postDataJSON() as { section_ids: string[] };
      const byID = new Map(state.editable_revision.sections.map((section) => [section.id, section]));
      state.editable_revision.sections = section_ids.map((id, position) => ({ ...byID.get(id)!, position }));
      return route.fulfill({ json: state.editable_revision });
    }
    if (request.method() === "PATCH" && url.pathname.endsWith("/lessons/order")) {
      if (failOrder()) {
        await new Promise((resolve) => setTimeout(resolve, 1_500));
        return route.fulfill({ status: 422, contentType: "application/problem+json", body: JSON.stringify({ type: "about:blank", title: "Validation failed", status: 422, code: "VALIDATION_FAILED", detail: "Order was rejected" }) });
      }
      const sectionID = url.pathname.split("/sections/")[1].split("/lessons")[0];
      const section = state.editable_revision.sections.find((candidate) => candidate.id === sectionID)!;
      const { lesson_ids } = request.postDataJSON() as { lesson_ids: string[] };
      const byID = new Map(section.lessons.map((lesson) => [lesson.id, lesson]));
      section.lessons = lesson_ids.map((id, position) => ({ ...byID.get(id)!, position }));
      return route.fulfill({ json: state.editable_revision });
    }
    return route.fulfill({ json: url.pathname === "/api/v1/courses" ? [state] : state });
  });
}

async function openCurriculum(page: Page, locale: "en" | "ar", viewport?: { width: number; height: number }) {
  if (viewport) await page.setViewportSize(viewport);
  await page.goto(`/${locale}/instructor/courses`);
  const toggle = page.getByTestId("authoring-toggle-CURRICULUM");
  if (await toggle.getAttribute("aria-expanded") !== "true") await toggle.click();
  await expect(page.getByTestId("curriculum")).toBeVisible();
}

async function keyboardMove(page: Page, testID: string, times: number, direction: "ArrowUp" | "ArrowDown") {
  const handle = page.getByTestId(testID);
  await handle.focus();
  await handle.press("Space");
  for (let index = 0; index < times; index += 1) await handle.press(direction);
  await handle.press("Space");
  await expect(handle).toBeFocused();
}

async function pointerDrag(page: Page, sourceTestID: string, targetTestID: string) {
  const sourceHandle = page.getByTestId(sourceTestID);
  const targetHandle = page.getByTestId(targetTestID);
  await sourceHandle.scrollIntoViewIfNeeded();
  const source = await sourceHandle.boundingBox();
  const target = await targetHandle.boundingBox();
  if (!source || !target) throw new Error("drag handle is not visible");
  const sourceX = source.x + source.width / 2;
  const sourceY = source.y + source.height / 2;
  await page.mouse.move(sourceX, sourceY);
  await page.mouse.down();
  await page.mouse.move(sourceX + 12, sourceY + 12, { steps: 3 });
  await page.waitForTimeout(100);
  await page.mouse.move(target.x + target.width / 2, target.y + target.height / 2, { steps: 20 });
  await page.mouse.up();
}

const visibleSectionOrder = (page: Page) => page.getByTestId("curriculum").locator("ol").first().locator(":scope > li").evaluateAll((rows) => rows.map((row) => row.getAttribute("data-testid")));
const visibleLessonOrder = (page: Page) => page.getByTestId(`section-${sectionIDs[0]}`).locator("ol").first().locator(":scope > li").evaluateAll((rows) => rows.map((row) => row.getAttribute("data-testid")));

test("D-102 pointer drag persists section order", async ({ page }) => {
  const state = studioCourse();
  await serveStudio(page, state);
  await openCurriculum(page, "en");
  await pointerDrag(page, `section-drag-handle-${sectionIDs[2]}`, `section-drag-handle-${sectionIDs[1]}`);
  await expect(page.getByTestId("curriculum-order-state")).toContainText("Order saved");
  expect(await visibleSectionOrder(page)).toEqual([`section-${sectionIDs[0]}`, `section-${sectionIDs[2]}`, `section-${sectionIDs[1]}`]);
});

test("D-102 keyboard section and same-section lesson order persists across reload", async ({ page }) => {
  const state = studioCourse();
  await serveStudio(page, state);
  await openCurriculum(page, "en");
  await keyboardMove(page, `section-drag-handle-${sectionIDs[2]}`, 2, "ArrowUp");
  await expect(page.getByTestId("curriculum-order-state")).toContainText("Order saved");
  expect(await visibleSectionOrder(page)).toEqual([`section-${sectionIDs[2]}`, `section-${sectionIDs[0]}`, `section-${sectionIDs[1]}`]);
  await keyboardMove(page, `lesson-drag-handle-${lessonIDs[2]}`, 1, "ArrowUp");
  expect(await visibleLessonOrder(page)).toEqual([`lesson-${lessonIDs[0]}`, `lesson-${lessonIDs[2]}`, `lesson-${lessonIDs[1]}`]);
  await page.reload();
  await openCurriculum(page, "en");
  expect(await visibleSectionOrder(page)).toEqual([`section-${sectionIDs[2]}`, `section-${sectionIDs[0]}`, `section-${sectionIDs[1]}`]);
});

test("D-102 rejected order rolls back without losing concurrent unsaved details", async ({ page }) => {
  const state = studioCourse();
  let reject = false;
  let reorderAttempts = 0;
  await serveStudio(page, state, () => {
    reorderAttempts += 1;
    return reject;
  });
  await openCurriculum(page, "en");
  reject = true;
  const handle = page.getByTestId(`section-drag-handle-${sectionIDs[2]}`);
  await handle.focus();
  await handle.press("Space");
  await handle.press("ArrowUp");
  await handle.press("ArrowUp");
  await handle.press("Space");
  await expect.poll(() => reorderAttempts).toBe(1);
  await page.getByTestId("authoring-toggle-BASICS").click();
  await page.getByTestId("revision-title-en").fill("Unsaved local title");
  await expect(page.getByTestId("curriculum-order-state")).toContainText("Couldn't reorder");
  expect(await visibleSectionOrder(page)).toEqual(sectionIDs.map((id) => `section-${id}`));
  await expect(page.getByTestId("revision-title-en")).toHaveValue("Unsaved local title");
});

for (const locale of ["en", "ar"] as const) {
  test(`D-102 ${locale} mobile keeps handles usable without horizontal overflow`, async ({ page }) => {
    await serveStudio(page, studioCourse());
    await openCurriculum(page, locale, { width: 390, height: 844 });
    await expect(page.getByTestId(`section-drag-handle-${sectionIDs[0]}`)).toBeVisible();
    await expect(page.getByTestId(`lesson-drag-handle-${lessonIDs[0]}`)).toBeVisible();
    const overflows = await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth);
    expect(overflows).toBe(false);
  });
}

test("D-102 non-editable revision exposes no reorder controls", async ({ page }) => {
  await serveStudio(page, studioCourse("PENDING_REVIEW"));
  await page.goto("/en/instructor/courses");
  await expect(page.locator('[data-testid^="section-drag-handle-"]')).toHaveCount(0);
  await expect(page.locator('[data-testid^="lesson-drag-handle-"]')).toHaveCount(0);
});

test("D-102 Arabic keyboard sorting announces the localized item", async ({ page }) => {
  await serveStudio(page, studioCourse());
  await openCurriculum(page, "ar");
  const handle = page.getByTestId(`section-drag-handle-${sectionIDs[2]}`);
  await handle.focus();
  await handle.press("Space");
  const announcement = page.locator('[aria-live="assertive"]').filter({ hasText: "القسم 3" });
  await expect(announcement).toHaveCount(1);
  await expect(announcement).not.toContainText("Section 3");
  await handle.press("Escape");
});
