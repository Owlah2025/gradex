import AxeBuilder from "@axe-core/playwright";
import { expect, test, type BrowserContext, type Page, type Route } from "@playwright/test";

/**
 * The Bundle card's member stack and the Admin management surface, exercised in
 * a real browser against mocked catalogue responses.
 *
 * Mocked, because what is under test here is the interaction and the state
 * reconciliation, not the SQL: the member counts this must be deterministic for
 * -- 1, 2, 3, 4 and more -- are awkward to seed and trivially expressible as
 * responses. The domain rules behind them are covered by the Go integration
 * suite instead.
 */

test.describe.configure({ timeout: 180_000 });

type Member = { position: number; title: string; slug: string };

function member(position: number): Member {
  return {
    position,
    title: `Stacked Course ${position + 1}`,
    slug: `course-stacked-${position + 1}`,
  };
}

function bundleOf(count: number, id: string, title: string) {
  return {
    id,
    slug: `bundle-${id.replace(/-/g, "")}`,
    title,
    description: "A Bundle under test.",
    course_count: count,
    price: { minor_units: 90000, regular_minor_units: 120000, offer_minor_units: 90000, currency: "KWD" },
    members: Array.from({ length: count }, (_, index) => {
      const item = member(index);
      return {
        course_id: `20000000-0000-0000-0000-0000000000${String(index + 1).padStart(2, "0")}`,
        slug: item.slug,
        title: item.title,
        instructor_display_name: "Gradex Instructor",
        subject: { label: "Computer Engineering", code: `CE-${index + 1}` },
        position: index,
      };
    }),
  };
}

async function useLocale(context: BrowserContext, locale: "ar" | "en"): Promise<void> {
  await context.addInitScript((value) => {
    window.localStorage.setItem("gradex.locale", value);
  }, locale);
}

/** Routes only the Bundle list, leaving every other landing call to the real API. */
async function mockBundleList(page: Page, bundles: unknown[]): Promise<void> {
  await page.route(
    (url) => url.pathname.endsWith("/api/v1/catalog/bundles"),
    (route: Route) => route.fulfill({ json: { items: bundles, page: 1, page_size: 20, total: bundles.length } }),
  );
}

function stack(page: Page, bundleID: string) {
  return page.locator(`[data-testid="bundle-card"][data-bundle-id="${bundleID}"] [data-testid="bundle-stack"]`);
}

function stackMember(page: Page, bundleID: string, index: number) {
  return stack(page, bundleID).locator(`[data-testid="bundle-stack-member"][data-member-index="${index}"]`);
}

/** The front member, read from the DOM's own z-order rather than from a guess. */
async function frontMemberIndex(page: Page, bundleID: string): Promise<number> {
  return stack(page, bundleID).evaluate((node) => {
    const members = Array.from(node.querySelectorAll('[data-testid="bundle-stack-member"]'));
    let best = -1;
    let bestZ = -Infinity;
    for (const element of members) {
      const zIndex = Number(getComputedStyle(element).zIndex || "0");
      if (zIndex > bestZ) {
        bestZ = zIndex;
        best = Number(element.getAttribute("data-member-index"));
      }
    }
    return best;
  });
}

const TWO = bundleOf(2, "30000000-0000-0000-0000-000000000002", "Two Course Bundle");
const THREE = bundleOf(3, "30000000-0000-0000-0000-000000000003", "Three Course Bundle");
const FOUR = bundleOf(4, "30000000-0000-0000-0000-000000000004", "Four Course Bundle");
const ONE = bundleOf(1, "30000000-0000-0000-0000-000000000001", "One Course Bundle");
const SIX = bundleOf(6, "30000000-0000-0000-0000-000000000006", "Six Course Bundle");

/**
 * The mandatory regression case. Production shipped a purely decorative,
 * `aria-hidden` stack with no handlers at all, so no member could ever be
 * promoted -- and a 2-member Bundle looked like a 3-member layout missing a
 * card.
 */
test("a two-Course Bundle promotes either member to the front on click", async ({ context, page }) => {
  await useLocale(context, "en");
  await page.setViewportSize({ width: 1280, height: 900 });
  await mockBundleList(page, [TWO]);
  await page.goto("/");

  const card = stack(page, TWO.id);
  await expect(card).toBeVisible();
  await expect(card.locator('[data-testid="bundle-stack-member"]')).toHaveCount(2);
  await expect(card).toHaveAttribute("data-active-member", "0");
  expect(await frontMemberIndex(page, TWO.id)).toBe(0);

  // Both members are genuinely on screen: the background one is not hidden
  // behind the front one, and it is hit-testable where it is drawn.
  const boxes = await Promise.all([0, 1].map((index) => stackMember(page, TWO.id, index).boundingBox()));
  expect(boxes[0]).not.toBeNull();
  expect(boxes[1]).not.toBeNull();
  expect(Math.abs((boxes[0]!.x) - (boxes[1]!.x))).toBeGreaterThan(20);

  await stackMember(page, TWO.id, 1).click();
  await expect(card).toHaveAttribute("data-active-member", "1");
  await expect(stackMember(page, TWO.id, 1)).toHaveAttribute("data-active", "true");
  expect(await frontMemberIndex(page, TWO.id)).toBe(1);

  // And back again, repeatedly: promotion is not a one-way door.
  await stackMember(page, TWO.id, 0).click();
  await expect(card).toHaveAttribute("data-active-member", "0");
  expect(await frontMemberIndex(page, TWO.id)).toBe(0);
  await stackMember(page, TWO.id, 1).click();
  await expect(card).toHaveAttribute("data-active-member", "1");

  // Selecting a member never navigates.
  expect(new URL(page.url()).pathname).toBe("/");

  // The promote controls are real, named, reachable controls -- the previous
  // stack was `aria-hidden` decoration with no semantics at all.
  const violations = await new AxeBuilder({ page })
    .include('[data-testid="bundle-card"]')
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
    .analyze();
  expect(violations.violations.map((violation) => `${violation.id}: ${violation.help}`).join("\n")).toBe("");
});

test("three- and four-Course stacks promote deterministically, and larger Bundles stay bounded", async ({ context, page }) => {
  await useLocale(context, "en");
  await page.setViewportSize({ width: 1440, height: 1000 });
  await mockBundleList(page, [THREE, FOUR, SIX]);
  await page.goto("/");

  for (const bundle of [THREE, FOUR]) {
    const expected = bundle.members.length;
    await expect(stack(page, bundle.id).locator('[data-testid="bundle-stack-member"]')).toHaveCount(expected);
    for (const index of [expected - 1, 1, 0]) {
      await stackMember(page, bundle.id, index).click();
      await expect(stack(page, bundle.id)).toHaveAttribute("data-active-member", String(index));
      expect(await frontMemberIndex(page, bundle.id)).toBe(index);
    }
  }

  // Beyond the bounded stack the remainder is stated, not crammed in.
  await expect(stack(page, SIX.id).locator('[data-testid="bundle-stack-member"]')).toHaveCount(4);
  await expect(
    page.locator(`[data-bundle-id="${SIX.id}"] [data-testid="bundle-stack-overflow"]`),
  ).toContainText("+2");
});

test("a one-Course Bundle renders one artwork rather than a stack of one", async ({ context, page }) => {
  await useLocale(context, "en");
  await page.setViewportSize({ width: 1280, height: 900 });
  await mockBundleList(page, [ONE]);
  await page.goto("/");

  await expect(stack(page, ONE.id).locator('[data-testid="bundle-stack-single"]')).toBeVisible();
  await expect(stack(page, ONE.id).locator('[data-testid="bundle-stack-member"]')).toHaveCount(0);
});

test("each Bundle keeps its own active member, and hover changes nothing", async ({ context, page }) => {
  await useLocale(context, "en");
  await page.setViewportSize({ width: 1440, height: 1000 });
  await mockBundleList(page, [TWO, THREE]);
  await page.goto("/");

  await stackMember(page, TWO.id, 1).click();
  await expect(stack(page, TWO.id)).toHaveAttribute("data-active-member", "1");
  // The neighbouring Bundle is untouched: state is per card, not global.
  await expect(stack(page, THREE.id)).toHaveAttribute("data-active-member", "0");

  await stackMember(page, THREE.id, 2).click();
  await expect(stack(page, THREE.id)).toHaveAttribute("data-active-member", "2");
  await expect(stack(page, TWO.id)).toHaveAttribute("data-active-member", "1");

  // Hover is not selection. A hover-driven stack is unusable on touch and
  // steals the selection back the moment the pointer drifts.
  await stackMember(page, TWO.id, 0).hover();
  await page.waitForTimeout(250);
  await expect(stack(page, TWO.id)).toHaveAttribute("data-active-member", "1");
});

test("keyboard promotes a background member and the active Course link navigates", async ({ context, page }) => {
  await useLocale(context, "en");
  await page.setViewportSize({ width: 1280, height: 900 });
  await mockBundleList(page, [TWO]);
  await page.goto("/");

  const background = stackMember(page, TWO.id, 1);
  await background.focus();
  await expect(background).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(stack(page, TWO.id)).toHaveAttribute("data-active-member", "1");

  await stackMember(page, TWO.id, 0).focus();
  await page.keyboard.press("Space");
  await expect(stack(page, TWO.id)).toHaveAttribute("data-active-member", "0");

  // Member buttons carry the Course name, so the promotion control is not an
  // unlabelled box to a screen reader.
  await expect(background).toHaveAccessibleName(new RegExp(TWO.members[1].title));

  // Navigation is a separate, explicit action on the active Course.
  const viewCourse = page.locator(`[data-bundle-id="${TWO.id}"] [data-testid="bundle-view-active-course"]`);
  await expect(viewCourse).toHaveAttribute("href", `/en/catalog/${TWO.members[0].slug}`);

  // Buttons and links are siblings: no interactive element nested in another.
  const nested = await page.evaluate((bundleID) => {
    const card = document.querySelector(`[data-bundle-id="${bundleID}"]`);
    if (!card) return -1;
    return Array.from(card.querySelectorAll("a, button")).filter((node) =>
      node.parentElement?.closest("a, button"),
    ).length;
  }, TWO.id);
  expect(nested).toBe(0);
});

test("the Arabic stack mirrors its offsets without reversing member identity", async ({ context, page }) => {
  await useLocale(context, "ar");
  await page.setViewportSize({ width: 1280, height: 900 });
  await mockBundleList(page, [THREE]);
  await page.goto("/");

  await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
  const geometry = await stack(page, THREE.id).evaluate((node) => {
    const members = Array.from(node.querySelectorAll('[data-testid="bundle-stack-member"]'));
    return members.map((element) => ({
      index: Number(element.getAttribute("data-member-index")),
      depth: Number(element.getAttribute("data-depth")),
      left: (element as HTMLElement).getBoundingClientRect().left,
    }));
  });
  // Member identity and order are untouched by direction...
  expect(geometry.map((entry) => entry.index)).toEqual([0, 1, 2]);
  // ...while the visual offsets run the other way: deeper cards sit further to
  // the left in RTL, where in LTR they sit further to the right.
  const byDepth = [...geometry].sort((a, b) => a.depth - b.depth);
  expect(byDepth[0].left).toBeGreaterThan(byDepth[1].left);
  expect(byDepth[1].left).toBeGreaterThan(byDepth[2].left);

  await stackMember(page, THREE.id, 2).click();
  await expect(stack(page, THREE.id)).toHaveAttribute("data-active-member", "2");
  expect(await frontMemberIndex(page, THREE.id)).toBe(2);
});

test.describe("touch input", () => {
  test.use({ hasTouch: true });

  test("the stack responds to taps at a mobile viewport", async ({ context, page }) => {
    await useLocale(context, "en");
    await page.setViewportSize({ width: 390, height: 844 });
    await mockBundleList(page, [TWO]);
    await page.goto("/");

    await stackMember(page, TWO.id, 1).tap();
    await expect(stack(page, TWO.id)).toHaveAttribute("data-active-member", "1");
    expect(await frontMemberIndex(page, TWO.id)).toBe(1);

    const widths = await page.evaluate(() => ({
      scroll: document.documentElement.scrollWidth,
      client: document.documentElement.clientWidth,
    }));
    expect(widths.scroll).toBeLessThanOrEqual(widths.client + 1);
  });
});

/* ------------------------------------------------------------------ */
/* Admin Bundles management surface                                     */
/* ------------------------------------------------------------------ */

type AdminBundleShape = {
  id: string;
  slug: string;
  lifecycle: "DRAFT" | "PUBLISHED" | "DELISTED" | "ARCHIVED";
  title_ar: string;
  title_en: string;
  description_ar: string;
  description_en: string;
  revision: number;
  course_count: number;
  eligible: boolean;
  deletable: boolean;
  price?: { regular_minor_units: number; offer_minor_units?: number | null; effective_minor_units: number; currency: "KWD" };
  members: Array<{ course_id: string; position: number; title_ar: string; title_en: string; instructor_display_name: string; effective_minor_units?: number | null }>;
  member_total_minor_units?: number | null;
  created_at: string;
  updated_at: string;
};

const NOW = "2026-09-01T10:00:00.000Z";

function adminBundle(overrides: Partial<AdminBundleShape> = {}): AdminBundleShape {
  return {
    id: "40000000-0000-0000-0000-000000000001",
    slug: "bundle-40000000000000000000000000000001",
    lifecycle: "DRAFT",
    title_ar: "باقة الاختبار",
    title_en: "Test Bundle",
    description_ar: "وصف",
    description_en: "Description",
    revision: 1,
    course_count: 2,
    eligible: true,
    deletable: true,
    price: { regular_minor_units: 120000, offer_minor_units: 90000, effective_minor_units: 90000, currency: "KWD" },
    members: [
      { course_id: "20000000-0000-0000-0000-000000000001", position: 0, title_ar: "مقرر أول", title_en: "Digital Logic", instructor_display_name: "Gradex Instructor", effective_minor_units: 60000 },
      { course_id: "20000000-0000-0000-0000-000000000002", position: 1, title_ar: "مقرر ثانٍ", title_en: "Programming", instructor_display_name: "Gradex Instructor", effective_minor_units: 60000 },
    ],
    member_total_minor_units: 120000,
    created_at: NOW,
    updated_at: NOW,
    ...overrides,
  };
}

async function mockAdminSession(page: Page): Promise<void> {
  await page.route("**/api/v1/session/bootstrap", (route) => route.fulfill({ json: { csrf_token: "csrf-token" } }));
  await page.route("**/api/v1/session", (route) =>
    route.fulfill({
      json: {
        status: "ACTIVE", role: "ADMIN", csrf_token: "csrf-token", display_name: "Test Admin",
        idle_expires_at: new Date(Date.now() + 3600000).toISOString(),
        absolute_expires_at: new Date(Date.now() + 7200000).toISOString(),
      },
    }),
  );
  await page.route("**/api/v1/me/academic-profile", (route) => route.fulfill({ status: 404, json: { status: 404 } }));
  await page.route("**/api/v1/admin/bundles/courses**", (route) =>
    route.fulfill({
      json: {
        items: adminBundle().members.map((item) => ({
          id: item.course_id, title_ar: item.title_ar, title_en: item.title_en,
          instructor_display_name: item.instructor_display_name,
          price: { regular_minor_units: 60000, effective_minor_units: 60000, currency: "KWD" },
        })),
        page: 1, page_size: 20, total: 2, has_next: false,
      },
    }),
  );
}

/** Serves the Bundle list from a mutable server-side-of-the-test state. */
async function serveAdminBundles(page: Page, state: { items: AdminBundleShape[] }): Promise<void> {
  await page.route(
    (url) => url.pathname.endsWith("/api/v1/admin/bundles"),
    (route: Route) => {
      if (route.request().method() !== "GET") return route.fallback();
      return route.fulfill({ json: { items: state.items } });
    },
  );
}

test("the Admin list shows each Bundle's members, price, savings and state", async ({ context, page }) => {
  await useLocale(context, "en");
  await page.setViewportSize({ width: 1440, height: 1100 });
  await mockAdminSession(page);
  const state = {
    items: [
      adminBundle(),
      adminBundle({
        id: "40000000-0000-0000-0000-000000000002", lifecycle: "PUBLISHED",
        title_en: "Live Bundle", deletable: false, revision: 4,
      }),
    ],
  };
  await serveAdminBundles(page, state);
  await page.goto("/en/admin/bundles");

  await expect(page.getByTestId("bundle-row")).toHaveCount(2);
  // A populated list must never render the empty state.
  await expect(page.getByTestId("bundle-list-empty")).toHaveCount(0);

  const draftRow = page.locator('[data-testid="bundle-row"][data-bundle-id="40000000-0000-0000-0000-000000000001"]');
  await expect(draftRow.getByTestId("bundle-lifecycle")).toHaveText("Draft");
  await expect(draftRow.getByTestId("bundle-members")).toContainText("Digital Logic");
  await expect(draftRow.getByTestId("bundle-members")).toContainText("Programming");
  await expect(draftRow.getByTestId("bundle-course-count")).toContainText("2");
  await expect(draftRow.getByTestId("offer-price")).toContainText("90.000 KWD");
  await expect(draftRow.getByTestId("bundle-member-total")).toContainText("120.000 KWD");
  await expect(draftRow.getByTestId("bundle-savings")).toContainText("30.000 KWD");
  await expect(draftRow.getByTestId("bundle-delete")).toBeVisible();

  // A Bundle with purchase history offers the lifecycle action and says why
  // hard deletion is not available, rather than offering a button that fails.
  const liveRow = page.locator('[data-testid="bundle-row"][data-bundle-id="40000000-0000-0000-0000-000000000002"]');
  await expect(liveRow.getByTestId("bundle-lifecycle")).toHaveText("Published");
  await expect(liveRow.getByTestId("bundle-delete")).toHaveCount(0);
  await expect(liveRow.getByTestId("bundle-delete-unavailable")).toBeVisible();
  await expect(liveRow.getByTestId("bundle-delist")).toBeVisible();
  await expect(liveRow.getByTestId("bundle-publish")).toHaveCount(0);
});

test("the Admin empty state appears only when there are genuinely no Bundles", async ({ context, page }) => {
  await useLocale(context, "en");
  await mockAdminSession(page);
  await serveAdminBundles(page, { items: [] });
  await page.goto("/en/admin/bundles");
  await expect(page.getByTestId("bundle-list-empty")).toBeVisible();
  await expect(page.getByTestId("bundle-row")).toHaveCount(0);
});

test("a price edit saves through the API and the list re-renders the authoritative value", async ({ context, page }) => {
  await useLocale(context, "en");
  await page.setViewportSize({ width: 1440, height: 1100 });
  await mockAdminSession(page);
  const state = { items: [adminBundle()] };
  await serveAdminBundles(page, state);
  await page.route(
    (url) => /\/api\/v1\/admin\/bundles\/[0-9a-f-]+$/.test(url.pathname),
    async (route: Route) => {
      if (route.request().method() === "GET") return route.fulfill({ json: state.items[0] });
      if (route.request().method() !== "PUT") return route.fallback();
      const body = route.request().postDataJSON();
      // The reason is required by the domain; the client must always send one
      // alongside a price.
      expect(body.price_reason).toBeTruthy();
      // Money crosses the wire as integer minor units, never as a float.
      expect(Number.isInteger(body.regular_price_minor_units)).toBe(true);
      const saved: AdminBundleShape = {
        ...state.items[0],
        revision: state.items[0].revision + 1,
        price: {
          regular_minor_units: body.regular_price_minor_units,
          offer_minor_units: body.offer_price_minor_units,
          effective_minor_units: body.offer_price_minor_units ?? body.regular_price_minor_units,
          currency: "KWD",
        },
      };
      state.items = [saved];
      return route.fulfill({ json: saved });
    },
  );
  await page.goto("/en/admin/bundles");

  await page.getByTestId("bundle-row").getByRole("button", { name: "Edit" }).click();
  await expect(page.locator("#bundle-regular")).toHaveValue("120000");

  // An offer at or above the regular price is refused before it reaches the API.
  await page.locator("#bundle-offer").fill("120000");
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.getByText("Offer price must be positive and lower than regular price.")).toBeVisible();

  // A price change without a reason is named, not collapsed into a generic failure.
  await page.locator("#bundle-offer").fill("75000");
  await page.locator("#bundle-price-reason").fill("");
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.getByText("A pricing change reason is required when you change the price.")).toBeVisible();

  await page.locator("#bundle-price-reason").fill("Autumn offer");
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.getByText("Bundle saved.")).toBeVisible();
  // The list shows what the server returned, not what was typed.
  await expect(page.getByTestId("bundle-row").getByTestId("offer-price")).toContainText("75.000 KWD");

  // And it survives a reload, because it was persisted rather than held in React.
  await page.reload();
  await expect(page.getByTestId("bundle-row").getByTestId("offer-price")).toContainText("75.000 KWD");
});

test("deleting a Bundle is confirmed by title, removes it, and survives a reload", async ({ context, page }) => {
  await useLocale(context, "en");
  await page.setViewportSize({ width: 1440, height: 1100 });
  await mockAdminSession(page);
  const state = { items: [adminBundle()] };
  await serveAdminBundles(page, state);
  let deleteRequests = 0;
  await page.route(
    (url) => /\/api\/v1\/admin\/bundles\/[0-9a-f-]+$/.test(url.pathname),
    (route: Route) => {
      if (route.request().method() !== "DELETE") return route.fallback();
      deleteRequests += 1;
      // The revision travels with the deletion so a stale tab cannot delete.
      expect(route.request().postDataJSON()).toEqual({ expected_revision: 1 });
      state.items = [];
      return route.fulfill({ status: 204, body: "" });
    },
  );
  await page.goto("/en/admin/bundles");

  await page.getByTestId("bundle-delete").click();
  const dialog = page.getByTestId("bundle-delete-dialog");
  await expect(dialog).toBeVisible();
  // The Bundle's own title is in the confirmation.
  await expect(dialog).toContainText("Test Bundle");

  // Cancelling changes nothing at all.
  await page.getByTestId("bundle-delete-cancel").click();
  await expect(dialog).toHaveCount(0);
  expect(deleteRequests).toBe(0);
  await expect(page.getByTestId("bundle-row")).toHaveCount(1);

  await page.getByTestId("bundle-delete").click();
  await page.getByTestId("bundle-delete-confirm").click();
  await expect(page.getByText("Bundle deleted.")).toBeVisible();
  await expect(page.getByTestId("bundle-row")).toHaveCount(0);
  await expect(page.getByTestId("bundle-list-empty")).toBeVisible();
  expect(deleteRequests).toBe(1);

  await page.reload();
  await expect(page.getByTestId("bundle-row")).toHaveCount(0);
});

test("a refused deletion explains itself and leaves the Bundle in place", async ({ context, page }) => {
  await useLocale(context, "en");
  await page.setViewportSize({ width: 1440, height: 1100 });
  await mockAdminSession(page);
  // The row was fetched as deletable, and a purchase request arrived since.
  const state = { items: [adminBundle()] };
  await serveAdminBundles(page, state);
  await page.route(
    (url) => /\/api\/v1\/admin\/bundles\/[0-9a-f-]+$/.test(url.pathname),
    (route: Route) => {
      if (route.request().method() !== "DELETE") return route.fallback();
      state.items = [adminBundle({ deletable: false })];
      return route.fulfill({
        status: 409,
        contentType: "application/problem+json",
        json: {
          type: "https://api.gradex.com/problems/bundle-referenced",
          title: "Bundle has purchase history", status: 409,
          detail: "This Bundle has been requested or purchased, so it cannot be deleted.",
          code: "BUNDLE_REFERENCED",
        },
      });
    },
  );
  await page.goto("/en/admin/bundles");

  await page.getByTestId("bundle-delete").click();
  await page.getByTestId("bundle-delete-confirm").click();
  await expect(page.getByRole("alert").filter({ hasText: /cannot be deleted/ })).toBeVisible();
  // The Bundle survives, and the surface reconciles to the server's answer.
  await expect(page.getByTestId("bundle-row")).toHaveCount(1);
  await expect(page.getByTestId("bundle-delete-unavailable")).toBeVisible();
});

test("a lifecycle change reconciles from the server and reports a conflict honestly", async ({ context, page }) => {
  await useLocale(context, "en");
  await page.setViewportSize({ width: 1440, height: 1100 });
  await mockAdminSession(page);
  const state = { items: [adminBundle()] };
  await serveAdminBundles(page, state);
  let conflictOnce = true;
  await page.route("**/api/v1/admin/bundles/*/publish", (route: Route) => {
    if (conflictOnce) {
      conflictOnce = false;
      // Someone else moved it first; the list must reload rather than paint an
      // optimistic state the server never agreed to.
      state.items = [adminBundle({ lifecycle: "PUBLISHED", revision: 9, deletable: true })];
      return route.fulfill({
        status: 409, contentType: "application/problem+json",
        json: {
          type: "https://api.gradex.com/problems/bundle-state-conflict",
          title: "Bundle changed", status: 409, detail: "Refresh the Bundle and try again.",
          code: "BUNDLE_STATE_CONFLICT",
        },
      });
    }
    return route.fulfill({ json: state.items[0] });
  });
  await page.route("**/api/v1/admin/bundles/*/delist", (route: Route) => {
    state.items = [adminBundle({ lifecycle: "DELISTED", revision: 10 })];
    return route.fulfill({ json: state.items[0] });
  });
  await page.goto("/en/admin/bundles");

  await page.getByTestId("bundle-publish").click();
  await expect(page.getByText(/changed elsewhere/)).toBeVisible();
  // The row now shows the authoritative lifecycle, and the actions follow it.
  await expect(page.getByTestId("bundle-lifecycle")).toHaveText("Published");
  await expect(page.getByTestId("bundle-delist")).toBeVisible();
  await expect(page.getByTestId("bundle-publish")).toHaveCount(0);

  await page.getByTestId("bundle-delist").click();
  await expect(page.getByTestId("bundle-lifecycle")).toHaveText("Hidden");
  await expect(page.getByTestId("bundle-publish")).toBeVisible();
});

test("a stale mutation response cannot overwrite the authoritative list", async ({ context, page }) => {
  await useLocale(context, "en");
  await page.setViewportSize({ width: 1440, height: 1100 });
  await mockAdminSession(page);
  const state = { items: [adminBundle()] };
  await serveAdminBundles(page, state);
  // The publish call succeeds but replies with a body that is already behind
  // the server's own state -- the shape an out-of-order or cached response
  // takes. The surface must reconcile from the authoritative list rather than
  // paint the reply it happened to receive.
  await page.route("**/api/v1/admin/bundles/*/publish", (route: Route) => {
    state.items = [adminBundle({ lifecycle: "PUBLISHED", revision: 2, deletable: true })];
    return route.fulfill({ json: adminBundle({ lifecycle: "DRAFT", revision: 1 }) });
  });
  await page.goto("/en/admin/bundles");

  await expect(page.getByTestId("bundle-lifecycle")).toHaveText("Draft");
  await page.getByTestId("bundle-publish").click();
  await expect(page.getByTestId("bundle-lifecycle")).toHaveText("Published");
  await expect(page.getByTestId("bundle-delist")).toBeVisible();
  // A reload agrees, so nothing was held only in React state.
  await page.reload();
  await expect(page.getByTestId("bundle-lifecycle")).toHaveText("Published");
});
