import { expect, test, type Browser, type Page } from "@playwright/test";

const kuwait4GEmulation = {
  name: "Kuwait 4G local emulation",
  latency: 170,
  downloadThroughput: (4 * 1024 * 1024) / 8,
  uploadThroughput: (1 * 1024 * 1024) / 8,
  connectionType: "cellular4g" as const,
};
const lcpThresholdMilliseconds = 2500;
const lcpSamples = 5;

const course = {
  id: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
  slug: "course-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  title: "Performance course",
  instructor_display_name: "Performance instructor",
  has_preview: false,
};
const detail = {
  ...course,
  description: "Performance course description.",
  sections: [{ title: "First section", position: 1, lesson_count: 1 }],
};

type PerformanceScenario = { path: string; detail: boolean };

const performanceScenarios: PerformanceScenario[] = [
  { path: "/en/catalog", detail: false },
  { path: `/en/catalog/${course.slug}`, detail: true },
];

test("production catalogue list and detail meet the local Kuwait 4G LCP budget", async ({ browser }) => {
  test.setTimeout(60_000);
  for (const scenario of performanceScenarios) {
    const samples: number[] = [];
    for (let sample = 0; sample < lcpSamples; sample++) {
      samples.push(await measureLargestContentfulPaint(browser, scenario));
    }
    const p95 = percentile(samples, 95);
    test.info().annotations.push({ type: "p95 LCP", description: `${scenario.path}: ${p95.toFixed(1)}ms` });
    console.log(`${kuwait4GEmulation.name} ${scenario.path} p95 LCP=${p95.toFixed(1)}ms`);
    expect(p95, `${scenario.path} p95 LCP`).toBeLessThan(lcpThresholdMilliseconds);
  }
});

async function measureLargestContentfulPaint(browser: Browser, scenario: PerformanceScenario) {
  const context = await browser.newContext();
  const page = await context.newPage();
  try {
    await prepareLargestContentfulPaint(page);
    await mockPublicCatalogue(page);
    await page.goto(scenario.path, { waitUntil: "networkidle" });
    await expect(scenario.detail ? page.getByRole("heading", { name: course.title, level: 1 }) : page.getByRole("link", { name: course.title })).toBeVisible();
    await page.waitForTimeout(100);
    const largestContentfulPaint = await page.evaluate(() => {
      return (window as Window & { s3LargestContentfulPaint?: number[] }).s3LargestContentfulPaint ?? [];
    });
    if (largestContentfulPaint.length === 0) {
      throw new Error(`no largest-contentful-paint entry for ${scenario.path}`);
    }
    return largestContentfulPaint.at(-1)!;
  } finally {
    await context.close();
  }
}

async function prepareLargestContentfulPaint(page: Page) {
  await page.addInitScript(() => {
    const values: number[] = [];
    new PerformanceObserver((entries) => {
      for (const entry of entries.getEntries()) values.push(entry.startTime);
    }).observe({ type: "largest-contentful-paint", buffered: true });
    (window as Window & { s3LargestContentfulPaint?: number[] }).s3LargestContentfulPaint = values;
  });
  const client = await page.context().newCDPSession(page);
  await client.send("Network.enable");
  await client.send("Network.emulateNetworkConditions", {
    offline: false,
    latency: kuwait4GEmulation.latency,
    downloadThroughput: kuwait4GEmulation.downloadThroughput,
    uploadThroughput: kuwait4GEmulation.uploadThroughput,
    connectionType: kuwait4GEmulation.connectionType,
  });
}

async function mockPublicCatalogue(page: Page) {
  await page.route("**/api/v1/catalog/courses", (route) => {
    return route.fulfill({ json: { items: [course], page: 1, page_size: 20, total: 1 } });
  });
  await page.route("**/api/v1/catalog/courses/**", (route) => route.fulfill({ json: detail }));
}

function percentile(samples: number[], percentileValue: number) {
  const sorted = [...samples].sort((left, right) => left - right);
  return sorted[Math.ceil((percentileValue / 100) * sorted.length) - 1];
}
