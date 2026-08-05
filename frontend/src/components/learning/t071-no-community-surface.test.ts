import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

/**
 * T071: D-046 absence over the Student-facing learning surface.
 *
 * The external Discord/Telegram Course community link left MVP scope and is deferred to S18. S5
 * renders none of it — and, just as importantly, renders no placeholder, no disabled control, and
 * no "coming soon" state, because a placeholder is the feature arriving early with the work left
 * undone.
 *
 * The backend halves — `backend/internal/learning/` and the S5 migrations — are asserted in the Go
 * suite, where mutation row 16 of the required-mutations table proves the detector is not vacuous.
 */

/**
 * The forbidden set. `community`, `discord`, and `telegram` are T071's three names verbatim; the
 * rest are the same capability renamed, which is how this decision would most plausibly erode.
 * Matching happens against text with every non-alphanumeric character removed, so `community_link`,
 * `communityUrl`, `CommunityLink`, `community-link`, and `community link` are one concept.
 */
const communityConcepts = [
  "community",
  "discord",
  "telegram",
  "whatsapp",
  "groupchat",
  "discussiongroup",
  "socialgroup",
  "sociallink",
  "joinourgroup",
  "comingsoon",
];

const normalize = (source: string): string => source.toLowerCase().replace(/[^a-z0-9]+/g, "");

function frontendRoot(): string {
  return process.cwd().endsWith("/frontend") ? process.cwd() : path.join(process.cwd(), "frontend");
}

/** Every production source file under one authoritative root. */
function productionFiles(relativeRoot: string): string[] {
  const root = path.join(frontendRoot(), relativeRoot);
  assert.ok(
    fs.existsSync(root) && fs.statSync(root).isDirectory(),
    `T071 scan root ${relativeRoot} is missing; the detector would pass vacuously`
  );
  const collected: string[] = [];
  const walk = (directory: string) => {
    for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
      const full = path.join(directory, entry.name);
      if (entry.isDirectory()) {
        walk(full);
        continue;
      }
      // Production only: a test that names the concept in order to forbid it is not the concept.
      if (/\.(ts|tsx)$/.test(entry.name) && !/\.test\.(ts|tsx)$/.test(entry.name)) {
        collected.push(full);
      }
    }
  };
  walk(root);
  return collected.sort();
}

const scanRoots = ["src/app/[locale]/learn", "src/components/learning"];

test("the Student learning surface carries no deferred community concept", () => {
  let scanned = 0;
  for (const relativeRoot of scanRoots) {
    const files = productionFiles(relativeRoot);
    assert.ok(files.length > 0, `T071 scan root ${relativeRoot} matched no files; the detector would pass vacuously`);
    scanned += files.length;

    for (const file of files) {
      // String literals, JSX text, object keys, and route paths are all in scope — only the
      // delimiters are normalized away, never the content.
      const normalized = normalize(fs.readFileSync(file, "utf8"));
      const relative = path.relative(frontendRoot(), file);
      for (const concept of communityConcepts) {
        assert.ok(
          !normalized.includes(concept),
          `${relative} carries the deferred community concept "${concept}"; D-046 defers it to S18, ` +
            `so S5 renders no link, no placeholder, and no coming-soon state`
        );
      }
    }
  }
  assert.ok(scanned >= 2, `T071 frontend scan covered only ${scanned} files`);
});

test("no learning API type exposes a community field", () => {
  // The read models are where a community link would arrive if the backend ever served one, so the
  // client contract is asserted as well as the screens that render it.
  const client = fs.readFileSync(path.join(frontendRoot(), "src/lib/api/learning.ts"), "utf8");
  const normalized = normalize(client);
  for (const concept of communityConcepts) {
    assert.ok(
      !normalized.includes(concept),
      `the learning API client declares the deferred community concept "${concept}"`
    );
  }
});

test("the concept scan recognises every spelling a rename would use", () => {
  for (const spelling of [
    "community_link",
    "communityLink",
    "CommunityURL",
    "community-url",
    "community link",
    "COMMUNITY_LINK",
    "discordInvite",
    "telegram_group",
    "Join our WhatsApp group",
    "groupChat",
    "discussion-group",
    "social_group",
    "Coming soon",
    '<a href="https://discord.gg/x">Join</a>',
  ]) {
    const normalized = normalize(spelling);
    assert.ok(
      communityConcepts.some((concept) => normalized.includes(concept)),
      `the concept scan missed "${spelling}" (normalized "${normalized}")`
    );
  }

  // And it stays usable: ordinary learning vocabulary must not trip it.
  for (const benign of [
    "lessonIdentity",
    "reportContext",
    "entitlementEvaluator",
    "courseHome",
    "commit the transaction",
    "communication is not the word",
  ]) {
    const normalized = normalize(benign);
    for (const concept of communityConcepts) {
      assert.ok(!normalized.includes(concept), `the concept scan false-positived on "${benign}" via "${concept}"`);
    }
  }
});
