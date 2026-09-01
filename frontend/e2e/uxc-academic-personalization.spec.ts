import {
  test,
  expect,
  request as playwrightRequest,
  type APIRequestContext,
  type BrowserContext,
  type Page,
  type TestInfo,
} from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import {
  issueRotatingSession,
  studentFor,
  ACADEMIC_ONBOARDING_TEST_SLOT,
  ACADEMIC_SKIP_TEST_SLOT,
} from "./rotating-students";
import { frontendOrigin } from "../src/lib/api/e2e-ports";

/**
 * UX-C — pre-signup academic personalisation, in a real browser.
 *
 * The academic hierarchy comes from the real Kuwait University launch manifest, imported through
 * the real Admin API, and every option the visitor chooses is read from the anonymous catalogue
 * endpoints. Nothing is injected into the frontend and no university, program or Course is invented
 * here: what is proven is the product against real data.
 *
 * The journey under test is the one an anonymous Student actually has — arrive, name their
 * university and program, browse a catalogue that reflects it, open a Course, come back, switch
 * language, leave and return — plus the two things that must *not* happen: the browsing preference
 * silently becoming account state, and a real saved profile being overwritten by it.
 */

const ADMIN = { email: "admin@example.test", accountID: "a0000000-0000-0000-0000-000000000000" };
const MANIFEST = "kuwait-university-launch-v1";
const ANY_INSTITUTION = "00000000-0000-0000-0000-000000000000";

const UNIVERSITY_SLUG = "kuwait-university";
const UNIVERSITY_EN = "Kuwait University";
const UNIVERSITY_AR = "جامعة الكويت";
const PROGRAM_SLUG = "computer-science";
const PROGRAM_EN = "Computer Science";
const PROGRAM_AR = "علوم الحاسوب";
const OTHER_PROGRAM_SLUG = "electrical-engineering";

const STORAGE_KEY = "gradex.academic-context";
const UUID_PATTERN = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i;

type StoredContext = {
  version: number;
  institutionSlug: string;
  programSlug: string;
  names: Record<string, string>;
};

async function ensureLaunchCatalog(): Promise<void> {
  const session = issueRotatingSession(ADMIN);
  const admin = await playwrightRequest.newContext({
    baseURL: frontendOrigin(),
    extraHTTPHeaders: {
      Accept: "application/json, application/problem+json",
      Origin: frontendOrigin(),
      Cookie: `${session.cookie_name}=${session.cookie_value}`,
      "X-CSRF-Token": session.csrf_token,
    },
  });
  const response = await admin.post(
    `/api/v1/admin/academic/institutions/${ANY_INSTITUTION}/import`,
    { data: { manifest: MANIFEST, mode: "apply" } },
  );
  expect(response.status(), await response.text()).toBe(200);
  await admin.dispose();
}

/** The real program list for the launch institution, so no option below is assumed to exist. */
async function publicPrograms(): Promise<{ slug: string; name_en: string }[]> {
  const api = await playwrightRequest.newContext({ baseURL: frontendOrigin() });
  const response = await api.get(
    `/api/v1/catalog/academic-options/institutions/${UNIVERSITY_SLUG}/programs`,
    { headers: { "Accept-Language": "en" } },
  );
  expect(response.status(), await response.text()).toBe(200);
  const body = (await response.json()) as { items: { slug: string; name_en: string }[] };
  await api.dispose();
  return body.items;
}

async function readStored(page: Page): Promise<StoredContext | null> {
  const raw = await page.evaluate(
    (key) => window.localStorage.getItem(key),
    STORAGE_KEY,
  );
  return raw === null ? null : (JSON.parse(raw) as StoredContext);
}

async function apiFor(session: ReturnType<typeof issueRotatingSession>): Promise<APIRequestContext> {
  return playwrightRequest.newContext({
    baseURL: frontendOrigin(),
    extraHTTPHeaders: {
      Accept: "application/json, application/problem+json",
      Origin: frontendOrigin(),
      Cookie: `${session.cookie_name}=${session.cookie_value}`,
      "X-CSRF-Token": session.csrf_token,
    },
  });
}

async function attachSession(
  context: BrowserContext,
  session: ReturnType<typeof issueRotatingSession>,
) {
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
}

/** Saves a real academic profile through the real authenticated surface, with real identifiers. */
async function saveComputerScienceProfile(api: APIRequestContext): Promise<void> {
  const institutions = await (await api.get("/api/v1/me/academic-options/institutions")).json();
  const institution = institutions.find(
    (item: { name_en: string }) => item.name_en === UNIVERSITY_EN,
  );
  expect(institution, "the launch catalog must expose Kuwait University").toBeTruthy();
  const colleges = await (
    await api.get(`/api/v1/me/academic-options/institutions/${institution.id}/colleges`)
  ).json();
  const science = colleges.find(
    (item: { name_en: string }) => item.name_en === "College of Science",
  );
  expect(science, "the launch catalog must expose the College of Science").toBeTruthy();
  const programs = await (
    await api.get(
      `/api/v1/me/academic-options/institutions/${institution.id}/programs?college_id=${science.id}`,
    )
  ).json();
  const chosen = programs.find((item: { name_en: string }) => item.name_en === PROGRAM_EN);
  expect(chosen, "the launch catalog must expose Computer Science").toBeTruthy();
  const saved = await api.put("/api/v1/me/academic-profile", {
    data: {
      institution_id: institution.id,
      enrollment_status: "ENROLLED",
      program_id: chosen.id,
      academic_unit_id: "",
      current_level: 2,
    },
  });
  expect(saved.status(), await saved.text()).toBe(200);
}

async function seedLocale(context: BrowserContext, locale: "ar" | "en") {
  await context.addInitScript((selected) => {
    window.localStorage.setItem("gradex.locale", selected);
  }, locale);
}

/**
 * Records console errors, so a passing journey cannot hide a broken one.
 *
 * A 401 is filtered out and only a 401. Every public surface here reads the signed-in Student's own
 * records — the academic profile, the access history — and being anonymous is the ordinary answer
 * to those, which the browser logs as a failed resource whatever the application does with it.
 * Treating that as a defect would make the assertion untrue rather than strict.
 */
function watchConsole(page: Page): string[] {
  const errors: string[] = [];
  const anonymousProbe = /status of 401/;
  page.on("console", (message) => {
    if (message.type() !== "error") return;
    if (anonymousProbe.test(message.text())) return;
    errors.push(message.text());
  });
  page.on("pageerror", (error) => errors.push(String(error)));
  return errors;
}

test.describe("UX-C anonymous academic personalisation", () => {
  test.beforeAll(async () => {
    await ensureLaunchCatalog();
  });

  test("A a first-time visitor names their university and gets a catalogue that reflects it", async ({
    browser,
  }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await seedLocale(context, "en");
    const page = await context.newPage();
    const errors = watchConsole(page);

    await page.goto("/");
    const picker = page.getByTestId("academic-picker");
    await expect(picker).toBeVisible();

    // The options are the real ones. Nothing here types a slug.
    const programs = await publicPrograms();
    expect(programs.length, "the launch manifest must expose programs").toBeGreaterThan(0);
    expect(programs.map((item) => item.slug)).toContain(PROGRAM_SLUG);

    const university = page.getByTestId("academic-picker-institution");
    await expect(university).toBeVisible();
    await university.selectOption({ label: UNIVERSITY_EN });

    const program = page.getByTestId("academic-picker-program");
    await expect(program).toContainText(PROGRAM_EN);
    await program.selectOption(PROGRAM_SLUG);

    await page.getByRole("button", { name: "Show my courses" }).click();

    // The catalogue is now addressed by the selection, so the link is shareable and Back works.
    await page.waitForURL(
      new RegExp(`/en/catalog\\?institution=${UNIVERSITY_SLUG}&program=${PROGRAM_SLUG}`),
    );
    const bar = page.getByTestId("catalogue-academic-context");
    await expect(bar).toBeVisible();
    await expect(bar.getByTestId("academic-context-names")).toContainText(UNIVERSITY_EN);
    await expect(bar.getByTestId("academic-context-names")).toContainText(PROGRAM_EN);

    // What was stored is the slug pair, and it says so is a device-local preference.
    const stored = await readStored(page);
    expect(stored?.version).toBe(1);
    expect(stored?.institutionSlug).toBe(UNIVERSITY_SLUG);
    expect(stored?.programSlug).toBe(PROGRAM_SLUG);
    await expect(bar.getByTestId("academic-context-provenance")).toContainText(
      "Saved on this device",
    );

    await bar.screenshot({ path: "playwright-report/uxc-academic-catalogue-bar-en.png" });
    await page.goto(`/ar/catalog?institution=${UNIVERSITY_SLUG}&program=${PROGRAM_SLUG}`);
    await expect(bar).toBeVisible();
    await bar.screenshot({ path: "playwright-report/uxc-academic-catalogue-bar-ar.png" });

    expect(errors, `console errors: ${errors.join(" | ")}`).toEqual([]);
    await context.close();
  });

  test("B only programs belonging to the chosen university are offered", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await seedLocale(context, "en");
    const page = await context.newPage();

    await page.goto("/");
    const university = page.getByTestId("academic-picker-institution");
    const program = page.getByTestId("academic-picker-program");

    await university.selectOption({ label: UNIVERSITY_EN });
    await expect(program).toContainText(PROGRAM_EN);

    const offered = await program.locator("option").evaluateAll((nodes) =>
      nodes.map((node) => (node as HTMLOptionElement).value).filter((value) => value !== ""),
    );
    const real = (await publicPrograms()).map((item) => item.slug);
    expect(offered.sort()).toEqual(real.sort());

    // No option is a raw identifier, and none is a translated label standing in for one.
    for (const value of offered) {
      expect(value).not.toMatch(UUID_PATTERN);
      expect(value).toMatch(/^[a-z0-9-]+$/);
    }
    await context.close();
  });

  test("C the context survives a Course visit and the way back", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await seedLocale(context, "en");
    const page = await context.newPage();
    const errors = watchConsole(page);

    await page.goto(`/en/catalog?institution=${UNIVERSITY_SLUG}&program=${PROGRAM_SLUG}`);
    await expect(page.getByTestId("catalogue-academic-context")).toBeVisible();

    // Landing page, reached by ordinary navigation, still knows the context.
    await page.goto("/");
    await expect(page.getByTestId("academic-context-panel-summary")).toBeVisible();
    await expect(
      page.getByTestId("academic-context-panel-summary").getByTestId("academic-context-names"),
    ).toContainText(UNIVERSITY_EN);
    await page
      .getByTestId("academic-context-panel-summary")
      .screenshot({ path: "playwright-report/uxc-academic-landing-summary-en.png" });

    // A Course page keeps a way back into the same narrowed catalogue.
    const firstCourse = page.getByTestId("featured-courses-list").getByRole("link").first();
    if (await firstCourse.count()) {
      await firstCourse.click();
      // The way back up is the breadcrumb's Courses crumb, which carries the
      // same academic context the standalone back link did.
      const back = page
        .getByTestId("breadcrumbs")
        .getByRole("link", { name: "Courses", exact: true });
      await expect(back).toBeVisible();
      await expect(back).toHaveAttribute(
        "href",
        `/en/catalog?institution=${UNIVERSITY_SLUG}&program=${PROGRAM_SLUG}`,
      );
      await back.click();
      await expect(page.getByTestId("catalogue-academic-context")).toBeVisible();
    }

    // And a fresh visit in the same browser restores it rather than asking again.
    await page.goto("/en/catalog");
    await page.waitForURL(new RegExp(`institution=${UNIVERSITY_SLUG}`));
    await expect(page.getByTestId("catalogue-academic-context")).toBeVisible();

    expect(errors, `console errors: ${errors.join(" | ")}`).toEqual([]);
    await context.close();
  });

  test("D switching language keeps the identity and changes only the words", async ({
    browser,
  }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await seedLocale(context, "en");
    const page = await context.newPage();

    await page.goto(`/en/catalog?institution=${UNIVERSITY_SLUG}&program=${PROGRAM_SLUG}`);
    const names = page.getByTestId("catalogue-academic-context").getByTestId("academic-context-names");
    await expect(names).toContainText(UNIVERSITY_EN);
    const before = await readStored(page);

    await page.goto(`/ar/catalog?institution=${UNIVERSITY_SLUG}&program=${PROGRAM_SLUG}`);
    await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
    await expect(names).toContainText(UNIVERSITY_AR);
    await expect(names).toContainText(PROGRAM_AR);

    // Same slugs, in the URL and in storage. The Arabic labels are display, never identity.
    const after = await readStored(page);
    expect(after?.institutionSlug).toBe(before?.institutionSlug);
    expect(after?.programSlug).toBe(before?.programSlug);
    expect(page.url()).toContain(`institution=${UNIVERSITY_SLUG}`);
    expect(page.url()).toContain(`program=${PROGRAM_SLUG}`);
    await context.close();
  });

  test("E changing the university drops a program that does not belong to it", async ({
    browser,
  }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await seedLocale(context, "en");
    const page = await context.newPage();

    await page.goto(`/en/catalog?institution=${UNIVERSITY_SLUG}&program=${PROGRAM_SLUG}`);
    const programFilter = page.getByLabel("Program", { exact: true });
    await expect(programFilter).toHaveValue(PROGRAM_SLUG);

    // Changing the program within the same university is an ordinary change.
    await programFilter.selectOption(OTHER_PROGRAM_SLUG);
    await page.waitForURL(new RegExp(`program=${OTHER_PROGRAM_SLUG}`));
    expect((await readStored(page))?.programSlug).toBe(OTHER_PROGRAM_SLUG);

    // Returning the university chooser to "all" cannot leave a program selected beneath it.
    await page.getByLabel("University", { exact: true }).selectOption("");
    await page.waitForURL((url) => !url.search.includes("institution="));
    expect(page.url()).not.toContain("program=");
    await context.close();
  });

  test("F the context can be cleared, and clearing it stays cleared", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await seedLocale(context, "en");
    const page = await context.newPage();

    await page.goto(`/en/catalog?institution=${UNIVERSITY_SLUG}&program=${PROGRAM_SLUG}`);
    await expect(page.getByTestId("catalogue-academic-context")).toBeVisible();
    expect(await readStored(page)).not.toBeNull();

    await page.getByTestId("academic-context-clear").click();
    await page.waitForURL((url) => !url.search.includes("institution="));
    await expect(page.getByTestId("catalogue-academic-context")).toHaveCount(0);
    expect(await readStored(page), "clearing must actually clear storage").toBeNull();

    // The reload is the real assertion: a context that came back here would be a trap.
    await page.reload();
    await expect(page).toHaveURL(/\/en\/catalog$/);
    await expect(page.getByTestId("catalogue-academic-context")).toHaveCount(0);
    await context.close();
  });

  test("G an academic context with no courses explains itself instead of going blank", async ({
    browser,
  }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await seedLocale(context, "en");
    const page = await context.newPage();

    // A real program with a Subject filter no published Course can satisfy.
    await page.goto(
      `/en/catalog?institution=${UNIVERSITY_SLUG}&program=${PROGRAM_SLUG}&subject=0000-000`,
    );
    // The active context stays on screen, so the reader can see what produced the empty result.
    await expect(page.getByTestId("catalogue-academic-context")).toBeVisible();
    await expect(page.getByRole("heading", { level: 3 })).toContainText(/No published courses/i);
    // The empty state's own way out, not the filter row's — the reader must be able to recover from
    // where they are looking.
    await expect(
      page.getByTestId("catalogue-empty").getByRole("button", { name: "Clear filters" }),
    ).toBeVisible();
    await context.close();
  });

  test("H corrupt and stale stored context fails safely", async ({ browser }) => {
    for (const corrupt of [
      "not json",
      "{}",
      '{"version":999,"institutionSlug":"kuwait-university"}',
      '{"version":1,"institutionSlug":""}',
      '{"version":1,"institutionSlug":"a-university-that-was-retired","programSlug":"x"}',
    ]) {
      const context = await browser.newContext({ locale: "en-US" });
      await seedLocale(context, "en");
      await context.addInitScript(
        ([key, value]) => window.localStorage.setItem(key, value),
        [STORAGE_KEY, corrupt] as const,
      );
      const page = await context.newPage();
      const errors = watchConsole(page);

      await page.goto("/en/catalog");
      // The catalogue still works, and asks again rather than acting on something it cannot vouch for.
      await expect(page.getByTestId("academic-filters")).toBeVisible();
      await expect(page.getByTestId("catalogue-academic-context")).toHaveCount(0);
      expect(errors, `${corrupt} produced console errors: ${errors.join(" | ")}`).toEqual([]);
      await context.close();
    }
  });

  test("I the anonymous choice never becomes account state on its own", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await seedLocale(context, "en");
    const page = await context.newPage();

    const writes: string[] = [];
    await page.route("**/api/v1/me/academic-profile**", async (route) => {
      const request = route.request();
      if (request.method() !== "GET") writes.push(`${request.method()} ${request.url()}`);
      await route.continue();
    });

    await page.goto("/");
    await page.getByTestId("academic-picker-institution").selectOption({ label: UNIVERSITY_EN });
    const program = page.getByTestId("academic-picker-program");
    await expect(program).toContainText(PROGRAM_EN);
    await program.selectOption(PROGRAM_SLUG);
    await page.getByRole("button", { name: "Show my courses" }).click();
    await page.waitForURL(new RegExp(`institution=${UNIVERSITY_SLUG}`));

    await page.goto("/register");
    await expect(page.getByRole("heading").first()).toBeVisible();

    // Choosing a context and walking to registration writes nothing to any account, and the
    // preference is still exactly the slug pair it always was.
    expect(writes, "an anonymous selection was written to an academic profile").toEqual([]);
    const stored = await readStored(page);
    expect(stored?.institutionSlug).toBe(UNIVERSITY_SLUG);
    expect(JSON.stringify(stored)).not.toMatch(UUID_PATTERN);
    await context.close();
  });

  test("J the picker is operable by keyboard alone and labelled for a screen reader", async ({
    browser,
  }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await seedLocale(context, "en");
    const page = await context.newPage();
    await page.goto("/");

    const university = page.getByTestId("academic-picker-institution");
    await expect(university).toBeVisible();
    // Every control is named by a real <label>, not by placement.
    await expect(page.getByLabel("University", { exact: true })).toBeVisible();
    await expect(page.getByLabel(/Program/)).toBeVisible();

    // The university is chosen explicitly rather than left to the single-institution shortcut. That
    // shortcut is real product behaviour, but it is conditional on the catalogue holding exactly one
    // institution — and earlier specs in this suite create more — so a keyboard test that depended
    // on it was asserting the fixture rather than the control.
    await university.selectOption({ label: UNIVERSITY_EN });

    // The program chooser is disabled until its options exist, and a disabled control is correctly
    // skipped by Tab. Wait for it to become operable rather than racing the request that fills it.
    const program = page.getByTestId("academic-picker-program");
    await expect(program).toBeEnabled();

    await university.focus();
    await expect(university).toBeFocused();
    await page.keyboard.press("Tab");
    await expect(program).toBeFocused();
    await page.keyboard.press("Tab");
    await expect(page.getByRole("button", { name: "Show my courses" })).toBeFocused();
    await context.close();
  });

  test("K Arabic renders right to left with Arabic option names", async ({ browser }) => {
    const context = await browser.newContext({ locale: "ar" });
    await seedLocale(context, "ar");
    const page = await context.newPage();

    await page.goto("/ar/catalog");
    await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
    const university = page.getByLabel("الجامعة", { exact: true });
    await expect(university).toContainText(UNIVERSITY_AR);
    await university.selectOption(UNIVERSITY_SLUG);
    await page.waitForURL(new RegExp(`institution=${UNIVERSITY_SLUG}`));
    await expect(
      page.getByTestId("catalogue-academic-context").getByTestId("academic-context-names"),
    ).toContainText(UNIVERSITY_AR);
    await context.close();
  });

  /**
   * The handoff. A Student who chose a context while anonymous and then signed in is shown what
   * they chose, beside the real authenticated options, and asked to confirm it — because there is
   * no contract that turns a public slug into the identifier the profile write requires.
   */
  test("M signing in shows the earlier choice as guidance, never as a saved profile", async ({
    browser,
  }, testInfo) => {
    const student = studentFor(testInfo, ACADEMIC_SKIP_TEST_SLOT);
    const session = issueRotatingSession(student);
    const context = await browser.newContext({ locale: "en-US" });
    await seedLocale(context, "en");
    await attachSession(context, session);
    await context.addInitScript(
      ([key, value]) => window.localStorage.setItem(key, value),
      [
        STORAGE_KEY,
        JSON.stringify({
          version: 1,
          institutionSlug: UNIVERSITY_SLUG,
          programSlug: PROGRAM_SLUG,
          names: {
            institutionAr: UNIVERSITY_AR,
            institutionEn: UNIVERSITY_EN,
            programAr: PROGRAM_AR,
            programEn: PROGRAM_EN,
          },
        }),
      ] as const,
    );
    const page = await context.newPage();

    await page.goto("/en/learn/academic-profile");
    const handoff = page.getByTestId("academic-profile-handoff");
    await expect(handoff).toBeVisible();
    await expect(handoff).toContainText("Confirm your academic profile");
    await expect(handoff.getByTestId("academic-profile-handoff-context")).toContainText(
      UNIVERSITY_EN,
    );
    await expect(handoff.getByTestId("academic-profile-handoff-context")).toContainText(PROGRAM_EN);
    // It says where that choice lived. It never claims the account has it.
    await expect(handoff).toContainText("remembered on this device");

    // And the authenticated choosers are genuinely unanswered: nothing was inferred from a name.
    await expect(page.getByTestId("profile-college")).toHaveValue("");
    await expect(page.getByTestId("profile-program")).toHaveValue("");

    const api = await apiFor(session);
    const profile = await (await api.get("/api/v1/me/academic-profile")).json();
    expect(profile.program_id ?? "", "an anonymous slug became account state").toBe("");
    await api.dispose();
    await context.close();
  });

  /**
   * Precedence. A Student who has actually saved a profile has told the server who they are, and a
   * preference some browser is holding must not quietly replace it or narrow around it.
   */
  test("N a real saved profile outranks a stale anonymous preference and survives it", async ({
    browser,
  }, testInfo) => {
    const student = studentFor(testInfo, ACADEMIC_ONBOARDING_TEST_SLOT);
    const session = issueRotatingSession(student);
    const api = await apiFor(session);

    // The profile is saved through the real authenticated surface, with real identifiers.
    const institutions = await (
      await api.get("/api/v1/me/academic-options/institutions")
    ).json();
    const institution = institutions[0];
    const colleges = await (
      await api.get(`/api/v1/me/academic-options/institutions/${institution.id}/colleges`)
    ).json();
    const science = colleges.find((item: { name_en: string }) => item.name_en === "College of Science");
    expect(science, "the launch catalog must expose the College of Science").toBeTruthy();
    const programs = await (
      await api.get(
        `/api/v1/me/academic-options/institutions/${institution.id}/programs?college_id=${science.id}`,
      )
    ).json();
    const chosen = programs.find((item: { name_en: string }) => item.name_en === PROGRAM_EN);
    expect(chosen, "the launch catalog must expose Computer Science").toBeTruthy();
    const saved = await api.put("/api/v1/me/academic-profile", {
      data: {
        institution_id: institution.id,
        enrollment_status: "ENROLLED",
        program_id: chosen.id,
        academic_unit_id: "",
        current_level: 2,
      },
    });
    expect(saved.status(), await saved.text()).toBe(200);

    const context = await browser.newContext({ locale: "en-US" });
    await seedLocale(context, "en");
    await attachSession(context, session);
    // A stale preference for a different program, left over from before they signed up.
    await context.addInitScript(
      ([key, value]) => window.localStorage.setItem(key, value),
      [
        STORAGE_KEY,
        JSON.stringify({
          version: 1,
          institutionSlug: UNIVERSITY_SLUG,
          programSlug: OTHER_PROGRAM_SLUG,
          names: {},
        }),
      ] as const,
    );
    const page = await context.newPage();

    const writes: string[] = [];
    await page.route("**/api/v1/me/academic-profile**", async (route) => {
      if (route.request().method() !== "GET") writes.push(route.request().method());
      await route.continue();
    });

    await page.goto("/en/catalog");
    await expect(page.getByTestId("academic-filters")).toBeVisible();

    // The catalogue is not narrowed by the browser's preference: their profile ranks results, and
    // narrowing would hide Courses the account never asked to hide.
    await expect(page).toHaveURL(/\/en\/catalog$/);
    await expect(page.getByTestId("catalogue-academic-context")).toHaveCount(0);
    await expect(page.getByText("Courses relevant to your program appear first.")).toBeVisible();

    // And the saved profile is untouched.
    expect(writes, "browsing rewrote a saved academic profile").toEqual([]);
    const after = await (await api.get("/api/v1/me/academic-profile")).json();
    expect(after.setup_state).toBe("COMPLETED");
    expect(after.program_name).toBe(PROGRAM_EN);
    expect(after.program_slug).toBe(PROGRAM_SLUG);

    await api.dispose();
    await context.close();
  });

  test("O the personalisation surfaces carry no detectable accessibility violation", async ({
    browser,
  }) => {
    for (const [name, url] of [
      ["landing", "/"],
      ["catalogue", `/en/catalog?institution=${UNIVERSITY_SLUG}&program=${PROGRAM_SLUG}`],
    ] as const) {
      const context = await browser.newContext({ locale: "en-US" });
      await seedLocale(context, "en");
      const page = await context.newPage();
      await page.goto(url);
      await expect(
        name === "landing"
          ? page.locator("#academic-context")
          : page.getByTestId("catalogue-academic-context"),
      ).toBeVisible();
      const results = await new AxeBuilder({ page })
        .include(name === "landing" ? "#academic-context" : "[data-testid='catalogue-academic-context']")
        .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
        .analyze();
      // The rule id alone is not enough to act on, so the failure carries the offending nodes.
      const detail = results.violations
        .flatMap((violation) =>
          violation.nodes.map(
            (node) => `${violation.id}: ${node.html} — ${node.failureSummary ?? ""}`,
          ),
        )
        .join("\n");
      expect(
        results.violations.map((violation) => `${name}: ${violation.id}`),
        detail,
      ).toEqual([]);
      await context.close();
    }
  });

  /**
   * The anonymous steady state: no request that can only ever be refused.
   *
   * `/me/academic-profile` is scoped to the signed-in principal, so for a visitor the browser has
   * already resolved as anonymous there is no answer it could return. Issuing it anyway and reading
   * the 401 is using a refusal as control flow: it costs a round trip on the busiest public page,
   * puts an expected error in every visitor's network log, and — because precedence could not be
   * decided until it landed — held the landing page's courses behind it.
   */
  test("P a confirmed anonymous visitor never asks for an academic profile", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await seedLocale(context, "en");
    await context.addInitScript(
      ([key, value]) => window.localStorage.setItem(key, value),
      [
        STORAGE_KEY,
        JSON.stringify({
          version: 1,
          institutionSlug: UNIVERSITY_SLUG,
          programSlug: PROGRAM_SLUG,
          names: {
            institutionAr: UNIVERSITY_AR,
            institutionEn: UNIVERSITY_EN,
            programAr: PROGRAM_AR,
            programEn: PROGRAM_EN,
          },
        }),
      ] as const,
    );
    const page = await context.newPage();

    const profileRequests: string[] = [];
    const refusals: string[] = [];
    page.on("request", (request) => {
      if (request.url().includes("/api/v1/me/academic-profile"))
        profileRequests.push(`${request.method()} ${request.url()}`);
    });
    // Scoped to the principal-scoped surfaces, which is the class this tranche is responsible for.
    // `/api/v1/session` answering 401 for a visitor is the pre-existing identity contract — it is
    // how the application learns it is anonymous in the first place, it predates this work, and it
    // is the very signal the profile read is now gated on. Widening this to every 401 would assert
    // that contract rather than this one.
    page.on("response", (response) => {
      if (response.status() === 401 && response.url().includes("/api/v1/me/"))
        refusals.push(response.url());
    });

    // The landing page, where the cost of a wasted round trip is highest.
    await page.goto("/");
    await expect(page.getByTestId("academic-context-panel-summary")).toBeVisible();
    // Personalisation still happens, from the stored preference alone.
    await expect(
      page.getByTestId("academic-context-panel-summary").getByTestId("academic-context-names"),
    ).toContainText(UNIVERSITY_EN);
    // And the courses resolved without waiting on a profile that cannot exist.
    await expect(page.getByTestId("featured-courses-loading")).toHaveCount(0);

    // And the catalogue, which reads the same context.
    await page.goto(`/en/catalog?institution=${UNIVERSITY_SLUG}&program=${PROGRAM_SLUG}`);
    await expect(page.getByTestId("catalogue-academic-context")).toBeVisible();

    expect(
      profileRequests,
      "an anonymous visitor asked for an academic profile that cannot exist",
    ).toEqual([]);
    expect(
      refusals,
      `a principal-scoped request was made for a visitor and refused: ${refusals.join(" | ")}`,
    ).toEqual([]);
    await context.close();
  });

  /**
   * The window before `/session` answers.
   *
   * Being unclassified is not being anonymous. While the session is unresolved the application
   * knows nothing about who this is, and acting on the stored preference then is how a Student with
   * a real saved profile can have it silently outranked by a browser.
   */
  test("Q an unresolved session is not treated as an anonymous one", async ({
    browser,
  }, testInfo) => {
    const student = studentFor(testInfo, ACADEMIC_ONBOARDING_TEST_SLOT);
    const session = issueRotatingSession(student);
    const api = await apiFor(session);
    await saveComputerScienceProfile(api);

    const context = await browser.newContext({ locale: "en-US" });
    await seedLocale(context, "en");
    await attachSession(context, session);
    await context.addInitScript(
      ([key, value]) => window.localStorage.setItem(key, value),
      [
        STORAGE_KEY,
        JSON.stringify({
          version: 1,
          institutionSlug: UNIVERSITY_SLUG,
          programSlug: OTHER_PROGRAM_SLUG,
          names: {},
        }),
      ] as const,
    );
    const page = await context.newPage();

    // The session resolves late, which is the condition under test rather than an accident of
    // timing. Nothing may conclude "anonymous" from the delay.
    let sessionResolved = false;
    await page.route("**/api/v1/session", async (route) => {
      await new Promise((resolve) => setTimeout(resolve, 1500));
      sessionResolved = true;
      await route.continue();
    });

    const ordering: string[] = [];
    page.on("request", (request) => {
      if (request.url().includes("/api/v1/me/academic-profile"))
        ordering.push(sessionResolved ? "profile-after-session" : "profile-before-session");
    });

    await page.goto("/en/catalog");
    await expect(page.getByTestId("academic-filters")).toBeVisible();

    // The stale preference never became a filter, at any point, including during the delay.
    await expect(page).toHaveURL(/\/en\/catalog$/);
    await expect(page.getByTestId("catalogue-academic-context")).toHaveCount(0);
    await expect(page.getByText("Courses relevant to your program appear first.")).toBeVisible();
    // The profile was asked for only once authentication was confirmed.
    expect(ordering.every((entry) => entry === "profile-after-session")).toBe(true);

    const after = await (await api.get("/api/v1/me/academic-profile")).json();
    expect(after.program_slug).toBe(PROGRAM_SLUG);
    await api.dispose();
    await context.close();
  });

  /**
   * The transition. Signing in is what makes the profile read legitimate, and it is still not what
   * turns the anonymous slugs into account state.
   */
  test("R once authentication is confirmed the profile is read, and still binds nothing", async ({
    browser,
  }, testInfo) => {
    const student = studentFor(testInfo, ACADEMIC_SKIP_TEST_SLOT);
    const session = issueRotatingSession(student);
    const context = await browser.newContext({ locale: "en-US" });
    await seedLocale(context, "en");
    await context.addInitScript(
      ([key, value]) => window.localStorage.setItem(key, value),
      [
        STORAGE_KEY,
        JSON.stringify({
          version: 1,
          institutionSlug: UNIVERSITY_SLUG,
          programSlug: PROGRAM_SLUG,
          names: {
            institutionAr: UNIVERSITY_AR,
            institutionEn: UNIVERSITY_EN,
            programAr: PROGRAM_AR,
            programEn: PROGRAM_EN,
          },
        }),
      ] as const,
    );
    const page = await context.newPage();

    const reads: string[] = [];
    const writes: string[] = [];
    page.on("request", (request) => {
      if (!request.url().includes("/api/v1/me/academic-profile")) return;
      (request.method() === "GET" ? reads : writes).push(request.method());
    });

    // Anonymous first: the preference personalises, and nothing is asked of any account.
    await page.goto("/");
    await expect(page.getByTestId("academic-context-panel-summary")).toBeVisible();
    expect(reads, "an anonymous visitor read a profile").toEqual([]);

    // Then the same browser becomes authenticated.
    await attachSession(context, session);
    await page.goto("/en/learn/academic-profile");
    await expect(page.getByTestId("academic-profile-form")).toBeVisible();
    await expect
      .poll(() => reads.length, { message: "an authenticated visitor never read its own profile" })
      .toBeGreaterThan(0);

    // The earlier choice is guidance. It is not an answer, and it was not written.
    await expect(page.getByTestId("academic-profile-handoff")).toBeVisible();
    await expect(page.getByTestId("profile-program")).toHaveValue("");
    expect(writes, "an anonymous slug was bound to an account").toEqual([]);
    const stored = await readStored(page);
    expect(JSON.stringify(stored)).not.toMatch(UUID_PATTERN);
    await context.close();
  });

  for (const viewport of [
    { name: "mobile", width: 390, height: 844 },
    { name: "tablet", width: 1024, height: 768 },
    { name: "desktop", width: 1440, height: 900 },
  ]) {
    for (const locale of ["en", "ar"] as const) {
      test(`L ${viewport.name} ${locale} — the selector fits its viewport`, async ({ browser }) => {
        const context = await browser.newContext({
          locale: locale === "ar" ? "ar" : "en-US",
          viewport: { width: viewport.width, height: viewport.height },
        });
        await seedLocale(context, locale);
        const page = await context.newPage();
        await page.goto("/");

        const picker = page.getByTestId("academic-picker");
        await expect(picker).toBeVisible();
        const box = (await picker.boundingBox())!;
        expect(box.width).toBeLessThanOrEqual(viewport.width);

        // No horizontal page scroll at any width: the usual failure of a filter row on a phone.
        const overflow = await page.evaluate(
          () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
        );
        expect(overflow, "the page scrolls sideways").toBeLessThanOrEqual(1);

        // The panel sits below the hero, so the evidence is the section itself rather than whatever
        // happens to be at the top of the viewport.
        await page.locator("#academic-context").scrollIntoViewIfNeeded();
        await page.locator("#academic-context").screenshot({
          path: `playwright-report/uxc-academic-${viewport.name}-${locale}.png`,
        });
        await context.close();
      });
    }
  }
});
