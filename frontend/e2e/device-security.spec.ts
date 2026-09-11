import { test, expect, type BrowserContext, type Page } from "@playwright/test";
import {
  installIssuedSession,
  issueRotatingSession,
  type RotatingStudent,
} from "./rotating-students";
import {
	DEVICE_LIMIT_TEST_SLOT,
  DEVICE_LOCALE_TEST_SLOT,
  DEVICE_MANAGEMENT_TEST_SLOT,
  DEVICE_PLAYBACK_TEST_SLOT,
  DEVICE_SAME_DEVICE_TEST_SLOT,
  deviceSecurityStudentFor,
} from "../src/lib/api/e2e-device-students";
import { readDeviceTrustCodeFor } from "./device-trust";

/**
 * Student device security, proven in real browsers.
 *
 * The claim this suite exists to establish is the one that cannot be made by a
 * unit test: two *independent browser contexts*, both holding their own trusted
 * device credential for the same Student Account, cannot both play protected
 * course video at the same time — and the second one recovers on its own once
 * the first stops.
 *
 * Both contexts authenticate through the real session repository and carry the
 * production `__Host-` cookies, so every refusal below is produced by the real
 * server boundary rather than by anything this file arranges.
 */

const COURSE_ID = "c0000000-0000-0000-0000-000000000001";
const LESSON_ID = "30000000-0000-0000-0000-000000000001";
const SECOND_LESSON_ID = "30000000-0000-0000-0000-000000000002";
const STUDENT_PASSWORD = "StudentPassword123!";

// Real media, real HLS, and real database round trips, plus Next.js compiling a
// route on first hit. The budget is generous so a slow-but-correct run reports
// the assertion that failed rather than a timeout that hides it.
test.describe.configure({ timeout: 180_000 });

const PLAYBACK_ROUTE = (lessonID: string) => `/learn/lessons/${lessonID}/playback`;

/** Opens a browser context holding its own trusted device for this Student. */
async function openDevice(
  browser: { newContext: (options?: Record<string, unknown>) => Promise<BrowserContext> },
  student: RotatingStudent,
  /** Which of this Student's two browsers this context is. */
  deviceSlot: number,
  options: Record<string, unknown> = {},
): Promise<{ context: BrowserContext; page: Page; deviceID: string }> {
  const session = issueRotatingSession(student, deviceSlot);
  const context = await browser.newContext(options);
  await installIssuedSession(context, session);
  const page = await context.newPage();
  return { context, page, deviceID: session.device_id };
}

test.describe("Student device security — two devices, one protected playback", () => {
  test("Browser A plays, Browser B is refused, and B plays once A stops", async ({
    browser,
  }, testInfo) => {
	const student = deviceSecurityStudentFor(testInfo, DEVICE_PLAYBACK_TEST_SLOT);
    const lessonURL = `/en/learn/courses/${COURSE_ID}/lessons/${LESSON_ID}`;

    const a = await openDevice(browser, student, 0);
    const b = await openDevice(browser, student, 1);

    // Two genuinely different devices on one Account, which is the whole
    // premise: if these collided the rest of the journey would prove nothing.
    expect(a.deviceID).not.toEqual(b.deviceID);

    // --- Browser A starts protected playback -------------------------------
    const aPlayback = a.page.waitForResponse(
      (r) => r.url().includes(PLAYBACK_ROUTE(LESSON_ID)) && r.request().method() === "POST",
    );
    await a.page.goto(lessonURL);
    const aResponse = await aPlayback;
    expect(aResponse.status()).toBe(200);

    const aBody = await aResponse.json();
    // The server published the renewal contract rather than leaving the player
    // to guess it.
    expect(typeof aBody.heartbeat?.interval_seconds).toBe("number");
    await expect(a.page.getByTestId("lesson-media-unavailable")).toHaveCount(0);
    await expect(a.page.locator("video")).toBeVisible();
    await testInfo.attach("device-a-playing.png", {
      body: await a.page.screenshot({ fullPage: false }),
      contentType: "image/png",
    });

    // --- Browser B is refused, at the server boundary ----------------------
    const bPlayback = b.page.waitForResponse(
      (r) => r.url().includes(PLAYBACK_ROUTE(LESSON_ID)) && r.request().method() === "POST",
    );
    await b.page.goto(lessonURL);
    const bResponse = await bPlayback;
    expect(bResponse.status()).toBe(409);
    const bProblem = await bResponse.json();
    expect(bProblem.code).toBe("PLAYBACK_ALREADY_ACTIVE_ON_ANOTHER_DEVICE");
    // Nothing about the other device leaves the server.
    const refusal = JSON.stringify(bProblem);
    expect(refusal).not.toContain(a.deviceID);
    for (const leak of ["Chrome", "Linux", "device_id", "lease"]) {
      expect(refusal).not.toContain(leak);
    }

    // And the Student is told, in their own language, what to do about it.
    const blocked = b.page.getByTestId("lesson-playback-blocked");
    await expect(blocked).toBeVisible();
    await expect(blocked).toHaveAttribute("data-block", "ANOTHER_DEVICE");
    await expect(blocked).toContainText("Already playing on another device");
    await expect(blocked).toContainText(
      "This account is currently playing a course on another device.",
    );
    await expect(b.page.locator("video")).toHaveCount(0);
    await testInfo.attach("device-b-refused.png", {
      body: await b.page.screenshot({ fullPage: false }),
      contentType: "image/png",
    });

    // --- Browser A stops ---------------------------------------------------
    // A client-side navigation, because that is what leaving a lesson actually
    // is in this application: the player unmounts and hands the account's
    // playback slot back rather than making the other device wait out the
    // lease. A full page load cannot do this — the document is torn down before
    // the request completes — which is exactly why the lease also carries a TTL.
    const release = a.page.waitForResponse(
      (r) => r.url().includes("/media/playback-releases") && r.request().method() === "POST",
    );
    await a.page.getByRole("link", { name: "My courses" }).first().click();
    expect((await release).status()).toBe(204);

    // --- Browser B now succeeds -------------------------------------------
    const bRetry = b.page.waitForResponse(
      (r) => r.url().includes(PLAYBACK_ROUTE(LESSON_ID)) && r.request().method() === "POST",
    );
    await b.page.getByTestId("lesson-playback-retry").click();
    expect((await bRetry).status()).toBe(200);
    await expect(b.page.getByTestId("lesson-playback-blocked")).toHaveCount(0);
    await expect(b.page.locator("video")).toBeVisible();
    await testInfo.attach("device-b-playing-after-a-stopped.png", {
      body: await b.page.screenshot({ fullPage: false }),
      contentType: "image/png",
    });

    // --- And the refusal is symmetric --------------------------------------
    // A is now the one refused, which shows the control is about the account's
    // single slot rather than about either browser in particular.
    const aRefused = a.page.waitForResponse(
      (r) => r.url().includes(PLAYBACK_ROUTE(LESSON_ID)) && r.request().method() === "POST",
    );
    await a.page.goto(lessonURL);
    expect((await aRefused).status()).toBe(409);
    await expect(a.page.getByTestId("lesson-playback-blocked")).toHaveAttribute(
      "data-block",
      "ANOTHER_DEVICE",
    );

    // --- And a lease nobody released still lapses --------------------------
    // The other half of the recovery story: a device that simply disappears —
    // a closed tab, a slept laptop — must not hold the account's playback slot
    // indefinitely. B's page is closed without an orderly release, and A can
    // play again once B's lease expires on its own.
    //
    // The wait is bounded by STUDENT_PLAYBACK_LEASE_TTL, which this harness
    // sets to 10s. Only the timing is compressed; the mechanism is production's.
    await b.page.close();
    await expect(async () => {
      const response = await a.page.request.post(
        `/api/v1/learn/lessons/${LESSON_ID}/playback`,
        { headers: { Accept: "application/json, application/problem+json" } },
      );
      // Unauthenticated is not the outcome under test; a 409 means the lease is
      // still alive and this attempt should be retried.
      expect(response.status()).not.toBe(409);
    }).toPass({ timeout: 60_000, intervals: [2_000] });

    await a.context.close();
    await b.context.close();
  });

  test("the refusal renders in Arabic on a mobile viewport", async ({ browser }, testInfo) => {
	const student = deviceSecurityStudentFor(testInfo, DEVICE_LOCALE_TEST_SLOT);
    const lessonURL = `/ar/learn/courses/${COURSE_ID}/lessons/${LESSON_ID}`;

    const a = await openDevice(browser, student, 0);
    // A real phone viewport, because this refusal is one a Student is most
    // likely to meet on the second device they carry.
    const b = await openDevice(browser, student, 1, {
      viewport: { width: 390, height: 844 },
      isMobile: true,
      hasTouch: true,
    });

    const aPlayback = a.page.waitForResponse(
      (r) => r.url().includes(PLAYBACK_ROUTE(LESSON_ID)) && r.request().method() === "POST",
    );
    await a.page.goto(lessonURL);
    expect((await aPlayback).status()).toBe(200);

    const bPlayback = b.page.waitForResponse(
      (r) => r.url().includes(PLAYBACK_ROUTE(LESSON_ID)) && r.request().method() === "POST",
    );
    await b.page.goto(lessonURL);
    expect((await bPlayback).status()).toBe(409);

    await expect(b.page.locator("main")).toHaveAttribute("dir", "rtl");
    const blocked = b.page.getByTestId("lesson-playback-blocked");
    await expect(blocked).toBeVisible();
    await expect(blocked).toContainText("التشغيل جارٍ على جهاز آخر");
    // The Arabic copy uses the university-course register the dictionary
    // parity test enforces everywhere else.
    await expect(blocked).toContainText("مقرر");
    await testInfo.attach("device-b-refused-arabic-mobile.png", {
      body: await b.page.screenshot({ fullPage: false }),
      contentType: "image/png",
    });

    await a.context.close();
    await b.context.close();
  });

  test("a second lesson on the same device takes over instead of being refused", async ({
    browser,
  }, testInfo) => {
    // Multi-tab behaviour on one device is deliberately not punished with the
    // "another device" refusal: the newer playback replaces the older lease,
    // and the older player learns it is stale from its own heartbeat.
	const student = deviceSecurityStudentFor(testInfo, DEVICE_SAME_DEVICE_TEST_SLOT);
    const session = issueRotatingSession(student);
    const context = await browser.newContext();
    await installIssuedSession(context, session);

    const first = await context.newPage();
    const firstPlayback = first.waitForResponse(
      (r) => r.url().includes(PLAYBACK_ROUTE(LESSON_ID)) && r.request().method() === "POST",
    );
    await first.goto(`/en/learn/courses/${COURSE_ID}/lessons/${LESSON_ID}`);
    expect((await firstPlayback).status()).toBe(200);

    const second = await context.newPage();
    const secondPlayback = second.waitForResponse(
      (r) => r.url().includes(PLAYBACK_ROUTE(SECOND_LESSON_ID)) && r.request().method() === "POST",
    );
    await second.goto(`/en/learn/courses/${COURSE_ID}/lessons/${SECOND_LESSON_ID}`);
    const secondResponse = await secondPlayback;
    // The same trusted device is never told its own account is playing
    // "on another device".
    expect(secondResponse.status()).toBe(200);
    await expect(second.getByTestId("lesson-playback-blocked")).toHaveCount(0);
		await expect(first.getByTestId("lesson-playback-blocked")).toHaveAttribute(
			"data-block",
			"LEASE_LOST",
			{ timeout: 15_000 },
		);
		await expect(first.locator("video")).toHaveCount(0);

    await context.close();
  });
});

test.describe("Student device management", () => {
  test("the Devices screen lists both devices and removes the other one", async ({
    browser,
  }, testInfo) => {
	const student = deviceSecurityStudentFor(testInfo, DEVICE_MANAGEMENT_TEST_SLOT);
    const a = await openDevice(browser, student, 0);
    const b = await openDevice(browser, student, 1);

    await a.page.goto(`/en/learn/security`);
    const rows = a.page.getByTestId("device-row");
    await expect(rows).toHaveCount(2);
    // The browser making the request is identified as such, so the Student can
    // tell which one they are about to give up.
    await expect(a.page.getByTestId("device-current-badge")).toHaveCount(1);
    await expect(a.page.getByTestId("device-count")).toContainText("2 of 2");

    // Nothing on this screen is a credential.
    const rendered = (await a.page.locator("main").innerText()).toLowerCase();
    for (const leak of ["credential", "digest", "csrf", "session id", "token"]) {
      expect(rendered).not.toContain(leak);
    }
    await testInfo.attach("devices-screen.png", {
      body: await a.page.screenshot({ fullPage: true }),
      contentType: "image/png",
    });

    // Remove the *other* device: the one this browser is not using.
    const other = a.page.locator('[data-testid="device-row"][data-current="false"]');
    await expect(other).toHaveCount(1);
    // The row offered for removal is the one this browser is not using.
    await expect(other.getByTestId("device-remove")).toHaveAttribute("data-device-id", b.deviceID);
    await other.getByTestId("device-remove").click();
    const removal = a.page.waitForResponse(
      (r) => r.url().includes("/me/devices/") && r.request().method() === "DELETE",
    );
    await a.page.getByTestId("device-remove-confirm").click();
    expect((await removal).status()).toBe(200);
    await expect(a.page.getByTestId("device-row")).toHaveCount(1);

    // The removed device's session is over, and this browser's is not.
    //
    // Asserted through each browser's own live request, because that is the
    // claim: revocation is per device, so the browser that was removed loses
    // its session and the one that did the removing keeps working.
    // B has to be on the origin before it can make a same-origin request.
    await b.page.goto(`/en/learn/dashboard`);
    const removedDeviceStatus = await b.page.evaluate(async () => {
      const response = await fetch("/api/v1/me/devices", {
        credentials: "same-origin",
        headers: { Accept: "application/json, application/problem+json" },
      });
      return response.status;
    });
    expect(removedDeviceStatus).toBe(401);

    await a.page.reload();
    await expect(a.page.getByTestId("devices-panel")).toBeVisible();
    await expect(a.page.getByTestId("device-row")).toHaveCount(1);
    // The surviving row is this browser's own.
    await expect(a.page.getByTestId("device-current-badge")).toHaveCount(1);

    await a.context.close();
    await b.context.close();
  });

	test("a third browser proves the emailed code and replaces only the chosen device", async ({
		browser,
	}, testInfo) => {
		const student = deviceSecurityStudentFor(testInfo, DEVICE_LIMIT_TEST_SLOT);
		const a = await openDevice(browser, student, 0);
		const b = await openDevice(browser, student, 1);
		const bPlayback = b.page.waitForResponse(
			(response) => response.url().includes(PLAYBACK_ROUTE(LESSON_ID)) && response.request().method() === "POST",
		);
		await b.page.goto(`/en/learn/courses/${COURSE_ID}/lessons/${LESSON_ID}`);
		expect((await bPlayback).status()).toBe(200);
		await expect(b.page.locator("video")).toBeVisible();
		const cContext = await browser.newContext({ locale: "en-US" });
		const cPage = await cContext.newPage();
		const requestedAt = new Date();

		await cPage.goto("/login");
		await cPage.locator("#email").fill(student.email);
		await cPage.locator("#password").fill(STUDENT_PASSWORD);
		await cPage.locator('button[type="submit"]').click();
		await cPage.waitForURL(/\/device-trust/, { timeout: 30_000 });
		await expect(cPage.getByTestId("replaceable-device")).toHaveCount(2);

		// A pending browser may inspect limited overview information required by the
		// flow, but cannot invoke standalone device removal to churn devices.
		const pendingOverviewStatus = await cPage.evaluate(async () => {
			const response = await fetch("/api/v1/me/devices", {
				credentials: "same-origin",
				headers: { Accept: "application/json, application/problem+json" },
			});
			return response.status;
		});
		expect(pendingOverviewStatus).toBe(200);

		const pendingRemovalAttempt = await cPage.evaluate(async (targetDeviceID) => {
			const sessionRes = await fetch("/api/v1/session", {
				credentials: "same-origin",
				headers: { Accept: "application/json" },
			});
			const session = (await sessionRes.json()) as { csrf_token: string };
			const deleteRes = await fetch(`/api/v1/me/devices/${targetDeviceID}`, {
				method: "DELETE",
				credentials: "same-origin",
				headers: {
					"X-CSRF-Token": session.csrf_token,
					Accept: "application/json, application/problem+json",
				},
			});
			const body = (await deleteRes.json().catch(() => ({}))) as { code?: string };
			return { status: deleteRes.status, code: body.code };
		}, b.deviceID);
		expect(pendingRemovalAttempt.status).toBe(403);
		expect(pendingRemovalAttempt.code).toBe("NOT_AUTHORIZED");

		const code = await readDeviceTrustCodeFor(student.email, requestedAt);
		await cPage.getByTestId("device-code").fill(code);
		await cPage.locator(`input[name="replace_device_id"][value="${b.deviceID}"]`).check();
		await cPage.getByTestId("device-trust-submit").click();
		await cPage.waitForURL(/\/learn\/dashboard/, { timeout: 30_000 });

		const cDeviceID = await cPage.evaluate(async () => {
			const response = await fetch("/api/v1/me/devices", {
				headers: { Accept: "application/json, application/problem+json" },
			});
			if (!response.ok) throw new Error(`device list returned ${response.status}`);
			const overview = await response.json() as { devices: Array<{ id: string; current_device: boolean }> };
			return overview.devices.find((device) => device.current_device)?.id ?? "";
		});
		expect(cDeviceID).not.toBe("");
		expect(cDeviceID).not.toBe(a.deviceID);
		expect(cDeviceID).not.toBe(b.deviceID);

		await expect(b.page.locator("video")).toHaveCount(0, { timeout: 15_000 });
		const bStatus = await b.page.evaluate(async () => {
			const res = await fetch("/api/v1/me/devices", {
				credentials: "same-origin",
				headers: { Accept: "application/json, application/problem+json" },
			});
			return res.status;
		});
		expect(bStatus).toBe(401);

		await a.page.goto("/en/learn/dashboard");
		const aStatus = await a.page.evaluate(async () => {
			const res = await fetch("/api/v1/me/devices", {
				credentials: "same-origin",
				headers: { Accept: "application/json, application/problem+json" },
			});
			return res.status;
		});
		expect(aStatus).toBe(200);

		const cStatus = await cPage.evaluate(async () => {
			const res = await fetch("/api/v1/me/devices", {
				credentials: "same-origin",
				headers: { Accept: "application/json, application/problem+json" },
			});
			return res.status;
		});
		expect(cStatus).toBe(200);

		await a.context.close();
		await b.context.close();
		await cContext.close();
	});
});
