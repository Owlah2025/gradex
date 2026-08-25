import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { validateFixtureManifest, validateProfile } from "./harness.mjs";

const ROOT = path.dirname(fileURLToPath(import.meta.url));
const DEFAULT_PROFILE = path.join(ROOT, "limited-paid-beta.json");

const args = process.argv.slice(2);
const profilePath = valueAfter("--profile") || DEFAULT_PROFILE;
const profile = JSON.parse(fs.readFileSync(profilePath, "utf8"));
validateProfile(profile);

if (args.includes("--fixture")) {
  const fixturePath = valueAfter("--fixture");
  const fixture = JSON.parse(fs.readFileSync(fixturePath, "utf8"));
  validateFixtureManifest(fixture, profile);
}

if (args.includes("--list") || args.includes("--dry-run")) {
  const requiredEnv = [
    "GRADEX_LOADTEST_PROFILE",
    "GRADEX_LOADTEST_PROFILE_FILE",
    "GRADEX_LOADTEST_TARGET_URL",
    "GRADEX_LOADTEST_FIXTURE_DIR",
    "GRADEX_LOADTEST_RESULTS_DIR",
    "GRADEX_LOADTEST_RUN_ID",
    "GRADEX_LOADTEST_REPETITION",
    "GRADEX_LOADTEST_RELEASE_ID",
    "GRADEX_LOADTEST_CONTAINER_IMAGE_ID",
    "GRADEX_LOADTEST_COMPOSE_PROJECT",
    "GRADEX_LOADTEST_HOST_CLASS",
    "GRADEX_LOADTEST_STORAGE_PROVIDER",
  ];
  process.stdout.write(JSON.stringify({
    profile: profile.profile,
    scenarios: Object.keys(profile.scenarios),
    required_environment_names: requiredEnv,
    no_traffic: true,
  }, null, 2) + "\n");
} else {
  process.stdout.write(`validated ${profile.profile}\n`);
}

function valueAfter(flag) {
  const index = args.indexOf(flag);
  return index === -1 ? null : args[index + 1];
}
