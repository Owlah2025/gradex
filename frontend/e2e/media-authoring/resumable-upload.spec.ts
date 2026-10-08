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
// seedLandingAcademicFixtures owns this institution and subject in every isolated run.
const institutionID = "91000000-0000-0000-0000-000000000001";
const subjectCode = "0418-101";

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

function multipartVideo(directory: string, name: string, freeMiB: number): string {
  const file = path.join(directory, name);
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
  // A valid ISO-BMFF free box makes this real, short video span several 8 MiB upload parts.
  const free = Buffer.alloc(freeMiB * 1024 * 1024);
  free.writeUInt32BE(free.length, 0);
  free.write("free", 4, "ascii");
  fs.appendFileSync(file, free);
  return file;
}

const PART_BYTES = 8 * 1024 * 1024;

/*
  Records what the Instructor actually saw, across reloads: every progress value the bar held,
  every phase, and whether the retrying / resuming lines ever appeared. Polling the DOM from the
  test would miss short-lived states; an observer inside the page does not.
*/
const recorder = () => {
  type Seen = { values: Array<{ at: number; value: number; phase: string }>; phases: string[]; retrying: boolean; resuming: string[] };
  const w = window as unknown as { __gx: Seen };
  w.__gx = { values: [], phases: [], retrying: false, resuming: [] };
  const scan = () => {
    const root = document.querySelector('[data-testid^="lesson-video-upload-"]');
    if (!root) return;
    const phase = root.querySelector("[data-upload-phase]")?.getAttribute("data-upload-phase") || "";
    if (phase && w.__gx.phases.at(-1) !== phase) w.__gx.phases.push(phase);
    const bar = root.querySelector('[role="progressbar"][aria-valuenow]');
    const value = bar ? Number(bar.getAttribute("aria-valuenow")) : NaN;
    if (!Number.isNaN(value) && w.__gx.values.at(-1)?.value !== value)
      w.__gx.values.push({ at: Date.now(), value, phase });
    if (root.querySelector('[data-testid="resumable-retrying"]')) w.__gx.retrying = true;
    const resuming = root.querySelector('[data-testid="resumable-resuming"]')?.textContent;
    if (resuming && w.__gx.resuming.at(-1) !== resuming) w.__gx.resuming.push(resuming);
  };
  const start = () => {
    new MutationObserver(scan).observe(document.body, { subtree: true, childList: true, attributes: true, characterData: true });
    scan();
  };
  if (document.body) start();
  else document.addEventListener("DOMContentLoaded", start);
};

async function throttleUploads(page: import("@playwright/test").Page, bytesPerSecond: number) {
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Network.enable");
  await cdp.send("Network.emulateNetworkConditions", {
    offline: false,
    latency: 0,
    downloadThroughput: -1,
    uploadThroughput: bytesPerSecond,
  });
}

test("byte progress, automatic retry, pause, reload recovery, wrong-file refusal, resume and cancel use real MinIO", async ({
  browser,
}, testInfo) => {
  const shot = (locator: import("@playwright/test").Locator, name: string) =>
    locator.screenshot({ path: testInfo.outputPath(`${name}.png`) }).catch(() => undefined);
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
    await page.addInitScript(recorder);
    await page.goto("/en/instructor/courses");

    // Use seeded academic identities; no other spec needs to create a subject first.
    // Create a dedicated course so we don't pollute the shared fixture and break s15
    await page.getByTestId("toggle-new-course").click();
    await page.getByTestId("new-course-institution").selectOption(institutionID);
    await page.getByTestId("new-course-subject-search").fill(subjectCode);
    const subject = page.getByTestId("new-course-subject-result").filter({
      hasText: `${subjectCode} · Introduction to Computer Science`,
    });
    await expect(subject).toHaveCount(1);
    await expect(subject).toBeVisible();
    await subject.click();
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
    const file = multipartVideo(directory, "resumable.mp4", 40);
    const size = fs.statSync(file).size;
    const partCount = Math.ceil(size / PART_BYTES);
    expect(partCount).toBeGreaterThanOrEqual(6);
    const wrongFile = multipartVideo(directory, "other.mp4", 12);

    const puts = new Map<number, number>();
    const creations: string[] = [];
    page.on("request", (request) => {
      const url = new URL(request.url());
      if (request.method() === "PUT" && url.searchParams.has("partNumber")) {
        const number = Number(url.searchParams.get("partNumber"));
        puts.set(number, (puts.get(number) || 0) + 1);
      }
    });
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
    // Part 2's first attempt is dropped once: a transient failure the client must absorb by itself.
    let dropPartTwo = true;
    await page.route(
      (url) => dropPartTwo && url.searchParams.get("partNumber") === "2",
      async (route) => {
        dropPartTwo = false;
        await route.abort("connectionfailed");
      },
    );
    await throttleUploads(page, 3 * 1024 * 1024);

    // 1. Upload begins and progresses in real bytes.
    await fileInput.setInputFiles(file);
    await expect(control.getByTestId("resumable-transfer")).toBeVisible({ timeout: 30000 });
    // The readout format; the bytes themselves are asserted to advance below.
    await expect(control.getByTestId("resumable-transfer")).toContainText(/^\d+(\.\d)? (B|KB|MB) \/ 42 MB/);
    await expect
      .poll(() => page.evaluate(() => (window as unknown as { __gx: { retrying: boolean } }).__gx.retrying), {
        message: "a transient part failure must show the automatic retry, not a failure",
        timeout: 30000,
      })
      .toBe(true);
    await expect(control.locator('[data-upload-phase="FAILED"]')).toHaveCount(0);
    await shot(control, "en-desktop-01-uploading");

    // 2. Pause once at least three parts are stored and later parts are in flight.
    await expect
      .poll(async () => (await ownerAPI.get(`/api/v1/media/uploads/${creations[0]}/multipart`).then((r) => r.json())).parts.length, {
        timeout: 90000,
      })
      .toBeGreaterThanOrEqual(3);
    await control.getByRole("button", { name: "Pause upload", exact: true }).click();
    const firstRun = await page.evaluate(() => (window as unknown as { __gx: { values: Array<{ value: number; phase: string }>; phases: string[] } }).__gx);
    const uploading = firstRun.values.filter((v) => v.phase === "UPLOADING").map((v) => v.value);
    expect(new Set(uploading).size, `observed ${uploading.join(",")}`).toBeGreaterThanOrEqual(8);
    for (let i = 1; i < uploading.length; i++) {
      expect(uploading[i], "healthy upload progress never moves backwards").toBeGreaterThanOrEqual(uploading[i - 1]);
      expect(uploading[i] - uploading[i - 1], `no whole-part jumps: ${uploading.join(",")}`).toBeLessThanOrEqual(10);
    }
    expect(creations).toHaveLength(1);
    expect(puts.get(2), "part 2 was retried automatically").toBe(2);

    // Ownership stays enforced on the paused session.
    const id = creations[0];
    const denied = await otherAPI.get(`/api/v1/media/uploads/${id}/multipart`);
    expect(denied.status(), await denied.text()).toBe(403);
    expect((await denied.json()).type).toContain("not-authorized");
    const deniedCancel = await otherAPI.delete(`/api/v1/media/uploads/${id}/multipart`);
    expect(deniedCancel.status(), await deniedCancel.text()).toBe(403);
    const serverParts: number[] = (await ownerAPI.get(`/api/v1/media/uploads/${id}/multipart`).then((r) => r.json())).parts.map(
      (p: { part_number: number }) => p.part_number,
    );
    const storedBytes = serverParts.reduce((sum, n) => sum + Math.min(PART_BYTES, size - (n - 1) * PART_BYTES), 0);
    const savedPercent = Math.floor((storedBytes / size) * 100);
    const saved = control.getByTestId("resumable-saved");
    await expect(saved).toContainText(`Upload paused at ${savedPercent}%`);
    await shot(control, "en-desktop-02-paused");
    const putsBeforeReload = new Map(puts);

    // 3. Reload: the File is gone, the saved upload is not.
    await page.reload();
    await page.getByTestId(`owned-course-${newCourseID}`).click();
    await openAuthoringSections(page);
    await expect(saved).toBeVisible();
    await expect(saved).toHaveAttribute("data-saved-source", "server");
    await expect(saved).toContainText(`Upload paused at ${savedPercent}%`);
    await expect(saved).toContainText("Reselect “resumable.mp4” to continue. Completed parts will not be uploaded again.");
    await expect(control.getByRole("button", { name: "Choose file and resume", exact: true })).toBeVisible();
    await expect(control.locator('[aria-valuenow="0"]'), "no false 0% after reload").toHaveCount(0);
    await shot(control, "en-desktop-03-recovered-after-reload");

    // 4. A different file is refused and starts nothing.
    await fileInput.setInputFiles(wrongFile);
    await expect(control.getByTestId(/lesson-video-message-/)).toContainText(
      "This is not the same file as the paused upload. Select “resumable.mp4” to continue",
    );
    expect(creations).toHaveLength(1);
    await expect(saved).toContainText(`Upload paused at ${savedPercent}%`);
    // The refused file is not kept as the in-tab file a Resume button would send.
    await expect(control.getByRole("button", { name: "Choose file and resume", exact: true })).toBeVisible();
    await expect(control.getByRole("button", { name: "Resume upload", exact: true })).toHaveCount(0);
    await shot(control, "en-desktop-04-wrong-file");

    // 5. The same file resumes from the stored parts, then verifies, processes and becomes READY.
    await throttleUploads(page, 6 * 1024 * 1024);
    await fileInput.setInputFiles(file);
    await expect(control.locator('[data-upload-phase="UPLOADING"]')).toBeVisible({ timeout: 60000 });
    await shot(control, "en-desktop-05-resumed-uploading");
    await expect(control.locator('[data-upload-phase="PROCESSING"], [data-upload-phase="VERIFYING"], [data-upload-phase="ATTACHING"], [data-upload-phase="READY"]')).toBeVisible({ timeout: 4 * 60 * 1000 });
    await shot(control, "en-desktop-06-after-upload");
    await expect(control.getByText("Video attached to this lesson.", { exact: true })).toBeVisible({
      timeout: 4 * 60 * 1000,
    });
    await shot(control, "en-desktop-07-ready");
    const resumed = await page.evaluate(() => (window as unknown as { __gx: { values: Array<{ value: number; phase: string }>; phases: string[]; resuming: string[] } }).__gx);
    const resumedUploading = resumed.values.filter((v) => v.phase === "UPLOADING").map((v) => v.value);
    expect(resumedUploading.length).toBeGreaterThan(0);
    expect(resumedUploading[0], `resumed progress starts at the stored ${savedPercent}%: ${resumedUploading.join(",")}`).toBeGreaterThanOrEqual(savedPercent);
    expect(Math.max(...resumedUploading)).toBe(100);
    expect(resumed.resuming).toContain(`Resuming from ${savedPercent}%…`);
    const order = resumed.phases;
    for (const expected of ["CHECKING", "UPLOADING", "VERIFYING", "ATTACHING", "READY"])
      expect(order, `phases seen: ${order.join(" → ")}`).toContain(expected);
    expect(order.indexOf("VERIFYING")).toBeGreaterThan(order.indexOf("UPLOADING"));
    for (const part of serverParts)
      expect(puts.get(part), `stored part ${part} was not uploaded again`).toBe(putsBeforeReload.get(part));
    for (let part = 1; part <= partCount; part++) expect(puts.get(part) || 0).toBeGreaterThan(0);
    expect(creations).toHaveLength(1);

    await page.reload();
    await page.getByTestId(`owned-course-${newCourseID}`).click();
    await openAuthoringSections(page);
    await expect(control.getByText("Video attached to this lesson.", { exact: true })).toBeVisible();
    await expect(saved).toHaveCount(0);

    // 6. A second upload is paused and explicitly cancelled, including its provider parts.
    await throttleUploads(page, 3 * 1024 * 1024);
    await fileInput.setInputFiles(file);
    await expect.poll(() => creations.length, { timeout: 60000 }).toBe(2);
    const cancelledID = creations[1];
    await expect(control.getByTestId("resumable-transfer")).toBeVisible({ timeout: 60000 });
    // In-tab pause keeps the File, so Resume needs no picker and continues from the stored parts.
    await expect
      .poll(async () => (await ownerAPI.get(`/api/v1/media/uploads/${creations[1]}/multipart`).then((r) => r.json())).parts.length, {
        timeout: 90000,
      })
      .toBeGreaterThanOrEqual(1);
    await control.getByRole("button", { name: "Pause upload", exact: true }).click();
    await expect(saved).toContainText("Resume to continue. Completed parts will not be uploaded again.");
    const pausedAt = Number(await saved.getAttribute("data-saved-percent"));
    expect(pausedAt).toBeGreaterThan(0);
    await control.getByRole("button", { name: "Resume upload", exact: true }).click();
    await expect(control.getByTestId("resumable-transfer")).toBeVisible({ timeout: 60000 });
    const resumedInTab = await page.evaluate(() => (window as unknown as { __gx: { values: Array<{ value: number; phase: string }> } }).__gx.values);
    expect(resumedInTab.filter((v) => v.phase === "UPLOADING").at(-1)!.value).toBeGreaterThanOrEqual(pausedAt);
    expect(creations, "resuming in the tab reuses the same server session").toHaveLength(2);
    const signed = await ownerAPI.post(`/api/v1/media/uploads/${cancelledID}/multipart/parts/2`, { data: {} });
    expect(signed.status(), await signed.text()).toBe(200);
    const signedURL = (await signed.json()).url;
    await control.getByRole("button", { name: "Cancel upload", exact: true }).click();
    await expect(control.getByTestId("resumable-cancelled")).toHaveText("Upload cancelled.");
    await shot(control, "en-desktop-08-cancelled");
    await expect(control.getByRole("button", { name: "Cancel upload", exact: true })).toHaveCount(0);
    await expect(saved).toHaveCount(0);
    const cancelled = await ownerAPI.get(`/api/v1/media/uploads/${cancelledID}/multipart`);
    expect(cancelled.status()).toBe(200);
    expect((await cancelled.json()).status).toBe("ABORTED");
    // The READY video from step 5 is untouched by cancelling a different upload.
    await page.reload();
    await page.getByTestId(`owned-course-${newCourseID}`).click();
    await openAuthoringSections(page);
    await expect(control.getByText("Video attached to this lesson.", { exact: true })).toBeVisible();
    await expect(saved).toHaveCount(0);
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

test("Arabic RTL on a phone: live progress, paused, recovered after reload, and cancel read correctly", async ({
  browser,
}, testInfo) => {
  test.setTimeout(6 * 60 * 1000);
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "gradex-resumable-ar-"));
  const session = issueRotatingSession(instructor);
  const ownerAPI = await api(session);
  const context = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });
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
  const shot = (locator: import("@playwright/test").Locator, name: string) =>
    locator.screenshot({ path: testInfo.outputPath(`${name}.png`) }).catch(() => undefined);
  try {
    const page = await context.newPage();
    await page.addInitScript(recorder);
    await page.goto("/ar/instructor/courses");
    await page.getByTestId("toggle-new-course").click();
    await page.getByTestId("new-course-institution").selectOption(institutionID);
    await page.getByTestId("new-course-subject-search").fill(subjectCode);
    const subject = page.getByTestId("new-course-subject-result");
    await expect(subject).toHaveCount(1);
    await subject.click();
    await page.getByTestId("new-course-title-ar").fill(`رفع عربي ${Date.now()}`);
    await page.getByTestId("new-course-title-en").fill(`Arabic Upload ${Date.now()}`);
    await page.getByTestId("new-course-description-ar").fill("وصف");
    await page.getByTestId("new-course-description-en").fill("Desc");
    await page.getByTestId("create-course").click();
    await expect(page.getByTestId("authoring-notice")).toBeVisible();
    await openAuthoringSections(page);
    const newCourseID = (await page.getByTestId("selected-course-context").getAttribute("data-course-id"))!;
    await page.getByTestId("section-title-ar").fill("القسم");
    await page.getByTestId("section-title-en").fill("Section");
    await page.getByTestId("add-section").click();
    const sectionRow = page.locator('[data-testid^="section-"]').first();
    await expect(sectionRow).toBeVisible();
    const sectionID = (await sectionRow.getAttribute("data-testid"))!.replace("section-", "");
    await page.getByTestId(`lesson-title-ar-${sectionID}`).fill("الدرس");
    await page.getByTestId(`lesson-title-en-${sectionID}`).fill("Lesson");
    await page.getByTestId(`add-lesson-${sectionID}`).click();
    const control = page.locator('[data-testid^="lesson-video-upload-"]').first();
    await expect(control).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.dir)).toBe("rtl");
    const file = multipartVideo(directory, "محاضرة.mp4", 40);
    const creations: string[] = [];
    page.on("response", async (response) => {
      if (
        response.request().method() === "POST" &&
        new URL(response.url()).pathname === "/api/v1/media/uploads/multipart" &&
        response.status() === 201
      )
        creations.push((await response.json()).asset_version_id);
    });
    await throttleUploads(page, 2 * 1024 * 1024);
    await control.locator('input[type="file"]').setInputFiles(file);
    const transfer = control.getByTestId("resumable-transfer");
    await expect(transfer).toBeVisible({ timeout: 30000 });
    await expect(transfer).toContainText("MB /");
    await shot(control, "ar-mobile-01-uploading");
    await expect
      .poll(async () => (await ownerAPI.get(`/api/v1/media/uploads/${creations[0]}/multipart`).then((r) => r.json())).parts.length, {
        timeout: 90000,
      })
      .toBeGreaterThanOrEqual(1);
    await control.getByRole("button", { name: "إيقاف مؤقت", exact: true }).click();
    const saved = control.getByTestId("resumable-saved");
    await expect(saved).toContainText("توقف الرفع مؤقتاً عند");
    await shot(control, "ar-mobile-02-paused");
    await page.reload();
    await page.getByTestId(`owned-course-${newCourseID}`).click();
    await openAuthoringSections(page);
    await expect(saved).toHaveAttribute("data-saved-source", "server");
    await expect(saved).toContainText("أعد اختيار");
    await expect(saved).toContainText("محاضرة.mp4");
    await expect(saved).toContainText("للمتابعة. لن يُعاد رفع الأجزاء المكتملة.");
    await expect(control.getByRole("button", { name: "اختر الملف للاستكمال", exact: true })).toBeVisible();
    // Nothing overflows the phone width.
    const overflow = await control.evaluate((element) => {
      const box = element.getBoundingClientRect();
      const offenders = Array.from(element.querySelectorAll("*"))
        .map((child) => ({ child, rect: child.getBoundingClientRect() }))
        .filter(({ rect }) => rect.width > 0 && (rect.left < box.left - 1 || rect.right > box.right + 1))
        .map(({ child, rect }) => `${child.tagName.toLowerCase()}[${child.getAttribute("data-testid") || child.className.toString().slice(0, 40)}] ${Math.round(rect.left - box.left)}..${Math.round(rect.right - box.right)}`);
      return { amount: element.scrollWidth - element.clientWidth, offenders };
    });
    expect(overflow.amount, overflow.offenders.join(" | ")).toBeLessThanOrEqual(1);
    await shot(control, "ar-mobile-03-recovered-after-reload");
    await control.getByRole("button", { name: "إلغاء الرفع", exact: true }).click();
    await expect(control.getByTestId("resumable-cancelled")).toHaveText("أُلغي الرفع.");
    await shot(control, "ar-mobile-04-cancelled");
    expect((await ownerAPI.get(`/api/v1/media/uploads/${creations[0]}/multipart`).then((r) => r.json())).status).toBe("ABORTED");
  } finally {
    await context.close();
    await ownerAPI.dispose();
    fs.rmSync(directory, { recursive: true, force: true });
  }
});
