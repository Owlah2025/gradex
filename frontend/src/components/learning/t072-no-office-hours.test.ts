import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

/**
 * T072: no office-hours element renders on ST05 or ST06 (spec §Assumptions).
 *
 * `docs/SCREENS.md` lists "upcoming office hours" as content on both the Student Dashboard and
 * Course Home. S5's specification defers it to S17 and is explicit about the shape of the deferral:
 * absent, "not rendered as an empty or 'coming soon' state". A greyed-out panel or a placeholder
 * heading would be the feature arriving early with the work missing, which is the outcome the
 * assumption exists to prevent.
 *
 * The assertion therefore follows the real render graph rather than a directory. Starting from the
 * two page files and walking their imports means a new office-hours component is caught the moment
 * it is wired into either screen — and that a component sitting unused elsewhere is not mistaken
 * for something ST05 or ST06 renders.
 */

/**
 * The forbidden set, matched against text with every non-alphanumeric character removed so
 * `office_hours`, `officeHours`, `OfficeHours`, `office-hours`, and `office hours` are one concept.
 *
 * Every entry is a compound. Bare `session`, `schedule`, `meet`, and `book` are deliberately absent:
 * these surfaces legitimately carry `authenticated_session`, `playback_session`, `SessionID`, and
 * `scheduleRetry`, and a detector that fired on those would be turned off rather than fixed.
 */
const officeHoursConcepts = [
  "officehour",
  "officesession",
  "upcomingsession",
  "livesession",
  "joinsession",
  "scheduledsession",
  "sessionschedule",
  "bookasession",
  "bookaslot",
  "webinar",
  "zoommeeting",
  "googlemeet",
  "meetinglink",
  "calendarinvite",
  "comingsoon",
];

const normalize = (source: string): string => source.toLowerCase().replace(/[^a-z0-9]+/g, "");

function frontendRoot(): string {
  return process.cwd().endsWith("/frontend") ? process.cwd() : path.join(process.cwd(), "frontend");
}

/** The two screens T072 names, by their production entry files. */
const screenEntries: Record<string, string> = {
  ST05: "src/app/[locale]/learn/dashboard/page.tsx",
  ST06: "src/app/[locale]/learn/courses/[courseId]/page.tsx",
};

/** Resolves one import specifier to a production file inside src, or null if it leaves the tree. */
function resolveImport(specifier: string, fromFile: string): string | null {
  const root = frontendRoot();
  let base: string;
  if (specifier.startsWith("@/")) {
    base = path.join(root, "src", specifier.slice(2));
  } else if (specifier.startsWith(".")) {
    base = path.resolve(path.dirname(fromFile), specifier);
  } else {
    // A package import — node_modules is not S5's surface.
    return null;
  }
  for (const candidate of [base, `${base}.tsx`, `${base}.ts`, path.join(base, "index.tsx"), path.join(base, "index.ts")]) {
    if (fs.existsSync(candidate) && fs.statSync(candidate).isFile()) return candidate;
  }
  return null;
}

/** Every production file one screen transitively renders. */
function renderGraph(entryRelative: string): string[] {
  const root = frontendRoot();
  const entry = path.join(root, entryRelative);
  assert.ok(fs.existsSync(entry), `T072 screen entry ${entryRelative} is missing; the detector would pass vacuously`);

  const seen = new Set<string>();
  const queue = [entry];
  while (queue.length > 0) {
    const file = queue.shift() as string;
    if (seen.has(file)) continue;
    seen.add(file);
    const source = fs.readFileSync(file, "utf8");
    // Both static imports and `import type` declarations; a type-only office-hours model still
    // means the concept reached the screen.
    for (const match of source.matchAll(/from\s+["']([^"']+)["']/g)) {
      const resolved = resolveImport(match[1], file);
      if (resolved && !seen.has(resolved)) queue.push(resolved);
    }
  }
  return [...seen].sort();
}

/**
 * Files each screen is known to render. Anchoring on these rather than a file count means a broken
 * import resolver fails loudly instead of quietly shrinking the graph to the entry file.
 */
const expectedInGraph: Record<string, string[]> = {
  ST05: [
    "src/components/learning/learning-views.tsx",
    "src/lib/api/learning.ts",
    "src/lib/i18n/dictionaries/en.ts",
    "src/lib/i18n/dictionaries/ar.ts",
  ],
  ST06: [
    "src/components/learning/learning-views.tsx",
    "src/components/learning/report-content-dialog.tsx",
    "src/lib/api/learning.ts",
    "src/lib/i18n/dictionaries/en.ts",
    "src/lib/i18n/dictionaries/ar.ts",
  ],
};

test("ST05 and ST06 render no office-hours element, empty state, or placeholder", () => {
  for (const [screen, entry] of Object.entries(screenEntries)) {
    const graph = renderGraph(entry);
    const relatives = graph.map((file) => path.relative(frontendRoot(), file));

    // The graph must actually reach what the screen is known to render.
    for (const expected of expectedInGraph[screen]) {
      assert.ok(
        relatives.includes(expected),
        `${screen}'s render graph is missing ${expected}; import resolution broke and the detector would pass vacuously`
      );
    }

    for (const file of graph) {
      const relative = path.relative(frontendRoot(), file);
      const normalized = normalize(fs.readFileSync(file, "utf8"));
      for (const concept of officeHoursConcepts) {
        assert.ok(
          !normalized.includes(concept),
          `${screen} renders ${relative}, which carries the office-hours concept "${concept}"; ` +
            `office hours are deferred to S17 and appear on neither screen, not even as an empty or coming-soon state`
        );
      }
    }
  }
});

test("the localized copy the two screens use names no office-hours element", () => {
  // A screen renders whatever its dictionary says. An unused-but-present "Office hours" string is a
  // placeholder waiting to be wired up, so the learning copy is asserted directly.
  for (const dictionary of ["src/lib/i18n/dictionaries/en.ts", "src/lib/i18n/dictionaries/ar.ts"]) {
    const source = fs.readFileSync(path.join(frontendRoot(), dictionary), "utf8");
    const normalized = normalize(source);
    for (const concept of officeHoursConcepts) {
      assert.ok(!normalized.includes(concept), `${dictionary} defines the office-hours concept "${concept}"`);
    }
  }
});

test("the Dashboard and Course Home read models carry no office-hours field", () => {
  // The payload is upstream of the element: a field arriving from the API is how an empty state
  // would appear first. The client contract for both screens is asserted alongside the screens.
  const client = fs.readFileSync(path.join(frontendRoot(), "src/lib/api/learning.ts"), "utf8");
  const normalized = normalize(client);
  for (const concept of officeHoursConcepts) {
    assert.ok(!normalized.includes(concept), `the learning API client declares the office-hours concept "${concept}"`);
  }
});

test("the concept scan catches office-hours spellings without firing on learning vocabulary", () => {
  for (const spelling of [
    "office_hours",
    "officeHours",
    "OfficeHours",
    "office-hours",
    "office hours",
    "OFFICE_HOURS",
    "upcomingSessions",
    "Upcoming sessions",
    "liveSession",
    "joinSession",
    "scheduled_session",
    "Book a slot",
    "webinarUrl",
    "zoomMeeting",
    "Google Meet",
    "meetingLink",
    "calendarInvite",
    "Coming soon",
    "Office hours — coming soon",
  ]) {
    const normalized = normalize(spelling);
    assert.ok(
      officeHoursConcepts.some((concept) => normalized.includes(concept)),
      `the concept scan missed "${spelling}" (normalized "${normalized}")`
    );
  }

  // The words these screens legitimately use must never trip it, or the detector gets disabled.
  for (const benign of [
    "authenticated_session",
    "playback_session",
    "sessionFromContext",
    "SessionID",
    "scheduleRetry",
    "currentCSRFToken",
    "courseHome",
    "lessonIdentity",
    "accessUntil",
    "bookmark",
    "meetsThreshold",
  ]) {
    const normalized = normalize(benign);
    for (const concept of officeHoursConcepts) {
      assert.ok(!normalized.includes(concept), `the concept scan false-positived on "${benign}" via "${concept}"`);
    }
  }
});
