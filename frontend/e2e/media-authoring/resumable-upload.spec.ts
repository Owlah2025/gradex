import {
  test,
  expect,
  request as playwrightRequest,
  type APIRequestContext,
} from "@playwright/test";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { issueRotatingSession } from "../rotating-students";
import { frontendOrigin } from "../../src/lib/api/e2e-ports";
import { openAuthoringSections } from "../authoring-sections";

const instructor = {
  email: "instructor@example.test",
  accountID: "a0000000-0000-0000-0000-000000000003",
};
const other = {
  email: "instructor-other@example.test",
  accountID: "a0000000-0000-0000-0000-000000000004",
};
const courseID = "c0000000-0000-0000-0000-000000000001";

function api(
  session: ReturnType<typeof issueRotatingSession>,
): Promise<APIRequestContext> {
  return playwrightRequest.newContext({
    baseURL: frontendOrigin(),
    extraHTTPHeaders: {
      Origin: frontendOrigin(),
      Cookie: `${session.cookie_name}=${session.cookie_value}`,
      "X-CSRF-Token": session.csrf_token,
      Accept: "application/json, application/problem+json",
    },
  });
}

function multipartVideo(directory: string): string {
  const file = path.join(directory, "resumable.mp4");
  execFileSync(
    "ffmpeg",
    [
      "-y",
      "-f",
      "lavfi",
      "-i",
      "testsrc=size=320x240:rate=15",
      "-t",
      "2",
      "-c:v",
      "libx264",
      "-preset",
      "ultrafast",
      "-pix_fmt",
      "yuv420p",
      "-movflags",
      "+faststart",
      file,
    ],
    { stdio: "ignore" },
  );
  // A valid ISO-BMFF free box makes this real, short video span three upload parts.
  const free = Buffer.alloc(18 * 1024 * 1024);
  free.writeUInt32BE(free.length, 0);
  free.write("free", 4, "ascii");
  fs.appendFileSync(file, free);
  return file;
}

test("multipart interruption, reload recovery, ownership and cancellation use real MinIO", async ({
  browser,
}) => {
  test.setTimeout(8 * 60 * 1000);
  const directory = fs.mkdtempSync(
    path.join(os.tmpdir(), "gradex-resumable-e2e-"),
  );
  const session = issueRotatingSession(instructor);
  const ownerAPI = await api(session);
  const otherAPI = await api(issueRotatingSession(other));
  const context = await browser.newContext();
  const origin = new URL(frontendOrigin());
  await context.addCookies([
    {
      name: session.cookie_name,
      value: session.cookie_value,
      domain: origin.hostname,
      path: "/",
      httpOnly: true,
      secure: true,
      sameSite: "Strict",
    },
  ]);
  try {
    const page = await context.newPage();
    await page.goto("/en/instructor/courses");
    
    // Create a dedicated course so we don't pollute the shared fixture and break s15
    await page.getByTestId("toggle-new-course").click();
    await page.getByTestId("new-course-institution").selectOption({ index: 1 });
    await page.getByTestId("new-course-subject-search").fill("CS101");
    await expect(page.getByTestId("new-course-subject-result")).toBeVisible();
    await page.getByTestId("new-course-subject-result").click();
    await page.getByTestId("new-course-title-ar").fill("دورة الرفع التجريبية");
    await page.getByTestId("new-course-title-en").fill(`Resumable Upload ${Date.now()}`);
    await page.getByTestId("new-course-description-ar").fill("وصف");
    await page.getByTestId("new-course-description-en").fill("Desc");
    await page.getByTestId("create-course").click();
    await expect(page.getByTestId("authoring-notice")).toContainText("Course created");
    await openAuthoringSections(page);
    const newCourseID = (await page.getByTestId("selected-course-context").getAttribute("data-course-id"))!;

    // Add Section
    await page.getByTestId("section-title-ar").fill("القسم");
    await page.getByTestId("section-title-en").fill("Media Section");
    await page.getByTestId("add-section").click();
    const sectionRow = page.locator('[data-testid^="section-"]').first();
    await expect(sectionRow).toBeVisible();
    await expect(sectionRow).toContainText("Media Section");
    const sectionID = (await sectionRow.getAttribute("data-testid"))!.replace("section-", "");

    // Add Lesson
    await page.getByTestId(`lesson-title-ar-${sectionID}`).fill("الدرس");
    await page.getByTestId(`lesson-title-en-${sectionID}`).fill("Media Lesson");
    await page.getByTestId(`add-lesson-${sectionID}`).click();
    await expect(sectionRow).toContainText("Media Lesson");
    const control = page
      .locator('[data-testid^="lesson-video-upload-"]')
      .first();
    const fileInput = control.locator('input[type="file"]');
    const file = multipartVideo(directory);
    let failSecond = true;
    const attempts = new Map<number, number>();
    const creations: string[] = [];
    page.on("response", async (response) => {
      if (
        response.request().method() === "POST" &&
        new URL(response.url()).pathname ===
          "/api/v1/media/uploads/multipart" &&
        response.status() === 201
      ) {
        creations.push((await response.json()).asset_version_id);
      }
    });
    await page.route(
      (url) => url.searchParams.has("partNumber"),
      async (route) => {
        const number = Number(
          new URL(route.request().url()).searchParams.get("partNumber"),
        );
        attempts.set(number, (attempts.get(number) || 0) + 1);
        if (number === 2 && failSecond) {
          await route.abort("connectionfailed");
          return;
        }
        await route.continue();
      },
    );
    await fileInput.setInputFiles(file);
    await expect(control.locator('[data-upload-phase="FAILED"]')).toBeVisible({
      timeout: 60000,
    });
    expect(creations).toHaveLength(1);
    expect(attempts.get(1)).toBe(1);
    expect(attempts.get(3)).toBe(1);
    const id = creations[0];
    const denied = await otherAPI.get(`/api/v1/media/uploads/${id}/multipart`);
    expect(denied.status(), await denied.text()).toBe(403);
    expect((await denied.json()).type).toContain("not-authorized");
    const deniedCancel = await otherAPI.delete(
      `/api/v1/media/uploads/${id}/multipart`,
    );
    expect(deniedCancel.status(), await deniedCancel.text()).toBe(403);
    const before = await ownerAPI.get(`/api/v1/media/uploads/${id}/multipart`);
    expect(before.status()).toBe(200);
    expect(
      (await before.json()).parts.map(
        (p: { part_number: number }) => p.part_number,
      ),
    ).toEqual([1, 3]);

    await page.reload();
    await page.getByTestId(`owned-course-${newCourseID}`).click();
    await openAuthoringSections(page);
    await expect(
      control.getByText(
        "A saved upload is available. Reselect the same file to resume.",
      ),
    ).toBeVisible();
    failSecond = false;
    await fileInput.setInputFiles(file);
    await expect(
      control.getByText("Video attached to this lesson.", { exact: true }),
    ).toBeVisible({ timeout: 4 * 60 * 1000 });
    expect(creations).toHaveLength(1);
    expect(attempts.get(1)).toBe(1);
    expect(attempts.get(3)).toBe(1);
    expect(attempts.get(2)).toBe(4);
    await page.reload();
    await page.getByTestId(`owned-course-${newCourseID}`).click();
    await openAuthoringSections(page);
    await expect(
      control.getByText("Video attached to this lesson.", { exact: true }),
    ).toBeVisible();

    // A second upload is interrupted and explicitly cancelled, including its provider parts.
    failSecond = true;
    await fileInput.setInputFiles(file);
    await expect(control.locator('[data-upload-phase="FAILED"]')).toBeVisible({
      timeout: 60000,
    });
    expect(creations).toHaveLength(2);
    const cancelledID = creations[1];
    const signed = await ownerAPI.post(
      `/api/v1/media/uploads/${cancelledID}/multipart/parts/2`,
      { data: {} },
    );
    expect(signed.status(), await signed.text()).toBe(200);
    const signedURL = (await signed.json()).url;
    await control
      .getByRole("button", { name: "Cancel upload", exact: true })
      .click();
    await expect(
      control.getByRole("button", { name: "Cancel upload", exact: true }),
    ).toHaveCount(0);
    const cancelled = await ownerAPI.get(
      `/api/v1/media/uploads/${cancelledID}/multipart`,
    );
    expect(cancelled.status()).toBe(200);
    expect((await cancelled.json()).status).toBe("ABORTED");
    const storage = await playwrightRequest.newContext();
    try {
      const refusedPart = await storage.put(signedURL, {
        data: Buffer.alloc(8 * 1024 * 1024),
        headers: { "Content-Type": "video/mp4" },
      });
      expect(refusedPart.status(), await refusedPart.text()).toBe(404);
      expect(await refusedPart.text()).toContain("NoSuchUpload");
    } finally {
      await storage.dispose();
    }
  } finally {
    await context.close();
    await ownerAPI.dispose();
    await otherAPI.dispose();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});
