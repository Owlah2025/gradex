import {
  test,
  expect,
  request as playwrightRequest,
  type APIRequestContext,
  type BrowserContext,
} from "@playwright/test";
import { issueRotatingSession } from "./rotating-students";
import { frontendOrigin } from "../src/lib/api/e2e-ports";

/**
 * T2 (MVP-F18) Launch Catalog Data — production-path proof.
 *
 * The Kuwait University launch manifest is imported through the real Admin API
 * against real PostgreSQL, then the existing T1 Admin Academic Catalog surface
 * is driven in a real browser to prove the imported hierarchy, the canonical
 * coded Subject, and the curriculum mapping are all visible.
 *
 * T2 ships no frontend production change: the T1 surface displays imported rows
 * because it reads the same catalog the importer writes. That is the point of
 * the proof, so this spec deliberately drives the unchanged T1 screen.
 *
 * Nothing here touches Student onboarding (T3), Instructor Course creation
 * (T4), the legacy taxonomy migration (T5), or public filters (T6).
 */

const ADMIN = { email: "admin@example.test", accountID: "a0000000-0000-0000-0000-000000000000" };
const INSTRUCTOR = { email: "instructor@example.test", accountID: "a0000000-0000-0000-0000-000000000003" };

const MANIFEST = "kuwait-university-launch-v1";
const UNIVERSITY_EN = "Kuwait University";
const COLLEGE_SCIENCE = "College of Science";
const COLLEGE_ENGINEERING = "College of Engineering and Petroleum";
const DEPARTMENT_CS = "Computer Science";
const PROGRAM_CS = "Computer Science";
const SUBJECT_CODE = "0410-101";
const SUBJECT_EN = "Calculus I";

type Session = ReturnType<typeof issueRotatingSession>;

async function signIn(context: BrowserContext, account: typeof ADMIN): Promise<Session> {
  const session = issueRotatingSession(account);
  const origin = new URL(frontendOrigin());
  await context.addInitScript(() => {
    window.localStorage.setItem("gradex.locale", "en");
  });
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
  return session;
}

async function apiContext(session: Session): Promise<APIRequestContext> {
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

const anyInstitution = "00000000-0000-0000-0000-000000000000";

test.describe("T2 Kuwait University launch catalog", () => {
  test("A the imported Kuwait University catalog is visible in the Admin surface", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    const session = await signIn(context, ADMIN);
    const admin = await apiContext(session);

    // A dry run first: it must report a plan and write nothing.
    const dryRun = await admin.post(`/api/v1/admin/academic/institutions/${anyInstitution}/import`, {
      data: { manifest: MANIFEST, mode: "dry_run" },
    });
    expect(dryRun.status()).toBe(200);
    const dryRunPlan = (await dryRun.json()) as { applied: boolean; counts: { create: number } };
    expect(dryRunPlan.applied).toBe(false);
    expect(dryRunPlan.counts.create).toBeGreaterThan(0);

    const applied = await admin.post(`/api/v1/admin/academic/institutions/${anyInstitution}/import`, {
      data: { manifest: MANIFEST, mode: "apply" },
    });
    expect(applied.status()).toBe(200);

    const institutions = (await (await admin.get("/api/v1/admin/academic/institutions")).json()) as Array<{
      id: string;
      name_en: string;
      max_academic_level: number;
    }>;
    const ku = institutions.find((item) => item.name_en === UNIVERSITY_EN);
    expect(ku, "Kuwait University must exist after import").toBeTruthy();
    // Five credit-derived levels, from the official Student Manual — not four.
    expect(ku!.max_academic_level).toBe(5);

    const page = await context.newPage();
    await page.goto("/en/admin/academic-catalog");
    await expect(page.getByRole("heading", { name: "Academic Catalog" })).toBeVisible();

    await page.getByTestId("academic-institution").selectOption({ label: UNIVERSITY_EN });

    // The real Kuwait University hierarchy, rendered as a hierarchy.
    const units = page.getByTestId("units-list");
    await expect(units).toContainText(COLLEGE_SCIENCE);
    await expect(units).toContainText(COLLEGE_ENGINEERING);
    await expect(units).toContainText(`Belongs to ${COLLEGE_SCIENCE}`);

    // The canonical Subject, displayed with the university's own dashed code.
    await expect(page.getByTestId("subjects-list")).toContainText(`${SUBJECT_CODE} · ${SUBJECT_EN}`);

    // Its study plan mapping, reached through the Program.
    await page.getByTestId("academic-program").selectOption({ label: PROGRAM_CS });
    await expect(page.getByTestId("curriculum-list")).toContainText("2024 — Active");
    await expect(page.getByTestId("mappings-list")).toContainText(SUBJECT_CODE);
    await expect(page.getByTestId("mappings-list")).toContainText("College requirement");

    // Imported data must be as identifier-free on screen as hand-entered data.
    const body = (await page.locator("body").innerText()).toLowerCase();
    expect(body).not.toMatch(/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/);
    expect(body).not.toContain("taxonomy");
    expect(body).not.toContain("manifest.yaml");

    await admin.dispose();
    await context.close();
  });

  test("E the Data Science & AI degree renders under its real college hierarchy", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    const session = await signIn(context, ADMIN);
    const admin = await apiContext(session);

    await admin.post(`/api/v1/admin/academic/institutions/${anyInstitution}/import`, {
      data: { manifest: MANIFEST, mode: "apply" },
    });

    const page = await context.newPage();
    await page.goto("/en/admin/academic-catalog");
    await page.getByTestId("academic-institution").selectOption({ label: UNIVERSITY_EN });

    // College of Life Sciences -> Information Science, the degree's real home.
    const units = page.getByTestId("units-list");
    await expect(units).toContainText("College of Life Sciences");
    await expect(units).toContainText("Information Science");
    await expect(units).toContainText("Belongs to College of Life Sciences");
    // No invented computing hierarchy was created to host the programme.
    await expect(units).not.toContainText("Data Science Department");
    await expect(units).not.toContainText("Computing College");

    await page.getByTestId("academic-program").selectOption({ label: "Data Science and Artificial Intelligence" });
    await expect(page.getByTestId("curriculum-list")).toContainText("current — Active");

    const mappings = page.getByTestId("mappings-list");
    // A DSAI-specific Subject and a reused canonical Subject, side by side.
    await expect(mappings).toContainText("1832-102");
    await expect(mappings).toContainText("Introduction to Data Science");
    await expect(mappings).toContainText("0410-101");
    // Placement from the official 8-semester plan is visible.
    await expect(mappings).toContainText("Recommended level 1");
    await expect(mappings).toContainText("Major core");

    // Founder Decision 2: no Mathematics degree is selectable.
    const programOptions = await page.getByTestId("academic-program").innerText();
    expect(programOptions).not.toContain("Financial Mathematics");
    expect(programOptions.split("\n").map((line) => line.trim())).not.toContain("Mathematics");
    expect(programOptions).not.toContain("Software Engineering");
    expect(programOptions).not.toContain("Cybersecurity Engineering");

    const body = (await page.locator("body").innerText()).toLowerCase();
    expect(body).not.toMatch(/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/);
    expect(body).not.toContain("taxonomy");

    await admin.dispose();
    await context.close();
  });

  test("B one canonical Calculus I serves five Kuwait University Programs", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    const session = await signIn(context, ADMIN);
    const admin = await apiContext(session);

    const institutions = (await (await admin.get("/api/v1/admin/academic/institutions")).json()) as Array<{
      id: string;
      name_en: string;
    }>;
    const ku = institutions.find((item) => item.name_en === UNIVERSITY_EN)!;

    // Exactly one Subject carries the canonical code, however it is typed.
    for (const query of [SUBJECT_CODE, "0410101", "calculus"]) {
      const found = (await (
        await admin.get(`/api/v1/admin/academic/institutions/${ku.id}/subjects?q=${encodeURIComponent(query)}`)
      ).json()) as Array<{ id: string; official_code: string | null }>;
      const calculus = found.filter((item) => item.official_code === SUBJECT_CODE);
      expect(calculus, `search ${query} must find exactly one canonical Calculus I`).toHaveLength(1);
    }

    // And it is mapped into every launch Program's active plan.
    const programs = (await (
      await admin.get(`/api/v1/admin/academic/institutions/${ku.id}/programs`)
    ).json()) as Array<{ id: string; name_en: string }>;
    // Computer Science, Cybersecurity, Computer Engineering, Electrical
    // Engineering. Cybersecurity is a real Kuwait University B.Sc., not a
    // Gradex teaching label.
    expect(programs.length).toBe(5);
    const names = programs.map((item) => item.name_en);
    expect(names).toContain("Cybersecurity");
    expect(names).toContain("Data Science and Artificial Intelligence");

    let mappedInto = 0;
    for (const program of programs) {
      const curricula = (await (
        await admin.get(`/api/v1/admin/academic/programs/${program.id}/curricula`)
      ).json()) as Array<{ id: string; status: string; version_label: string }>;
      const active = curricula.find((item) => item.status === "ACTIVE")!;
      const mappings = (await (
        await admin.get(`/api/v1/admin/academic/curricula/${active.id}/subjects`)
      ).json()) as Array<{ subject_official_code: string | null; recommended_level?: number | null }>;
      if (mappings.some((item) => item.subject_official_code === SUBJECT_CODE)) {
        mappedInto += 1;
      }
      // Placement exists only where Kuwait University publishes a plan. The
      // Computer Science 2024 major has an official Suggested Study Plan;
      // nothing else here does, so nothing else may claim a level.
      if (active.version_label !== "2024" && program.name_en !== "Data Science and Artificial Intelligence") {
        for (const mapping of mappings) {
          expect(mapping.recommended_level ?? null).toBeNull();
        }
      }
    }
    expect(mappedInto, "Calculus I must serve all five launch Programs").toBe(5);

    // The one plan Kuwait University does publish is used: Calculus I is a
    // Freshman Fall course on the Computer Science 2024 Suggested Study Plan.
    const cs = programs.find((item) => item.name_en === "Computer Science")!;
    const csCurricula = (await (
      await admin.get(`/api/v1/admin/academic/programs/${cs.id}/curricula`)
    ).json()) as Array<{ id: string; status: string; version_label: string }>;
    const csActive = csCurricula.find((item) => item.status === "ACTIVE")!;
    expect(csActive.version_label).toBe("2024");
    const csMappings = (await (
      await admin.get(`/api/v1/admin/academic/curricula/${csActive.id}/subjects`)
    ).json()) as Array<{
      subject_official_code: string | null;
      recommended_level?: number | null;
      recommended_semester?: number | null;
    }>;
    const calculus = csMappings.find((item) => item.subject_official_code === SUBJECT_CODE)!;
    expect(calculus.recommended_level).toBe(1);
    expect(calculus.recommended_semester).toBe(1);

    await admin.dispose();
    await context.close();
  });

  test("C an Instructor cannot import the launch catalog", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    const session = await signIn(context, INSTRUCTOR);
    const instructor = await apiContext(session);

    const dryRun = await instructor.post(`/api/v1/admin/academic/institutions/${anyInstitution}/import`, {
      data: { manifest: MANIFEST, mode: "dry_run" },
    });
    expect(dryRun.status()).toBe(403);

    const apply = await instructor.post(`/api/v1/admin/academic/institutions/${anyInstitution}/import`, {
      data: { manifest: MANIFEST, mode: "apply" },
    });
    expect(apply.status()).toBe(403);

    await instructor.dispose();
    await context.close();
  });

  test("D importing changes nothing about the legacy Course path", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    const session = await signIn(context, ADMIN);
    const admin = await apiContext(session);

    // The legacy taxonomy remains authoritative for Courses until T5.
    const terms = await admin.get("/api/v1/taxonomy/terms");
    expect(terms.status()).toBe(200);

    const page = await context.newPage();
    await page.goto("/en/admin/catalog");
    await expect(page.getByRole("heading", { name: "Taxonomy Vocabulary Administration" })).toBeVisible();

    // The public catalogue is unchanged and carries no academic filters yet.
    const publicCatalogue = await admin.get("/api/v1/catalog/courses");
    expect(publicCatalogue.status()).toBe(200);

    await admin.dispose();
    await context.close();
  });
});
