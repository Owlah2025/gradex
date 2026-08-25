import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { evaluateCapacityEvidence } from "./harness.mjs";

const ROOT = path.dirname(fileURLToPath(import.meta.url));
const profilePath = valueAfter("--profile") || path.join(ROOT, "limited-paid-beta.json");
const summaryPath = valueAfter("--summary");
if (!summaryPath) throw new Error("--summary is required");
const profile = JSON.parse(fs.readFileSync(profilePath, "utf8"));
const evidence = JSON.parse(fs.readFileSync(summaryPath, "utf8"));
const result = evaluateCapacityEvidence(evidence, profile);
process.stdout.write(JSON.stringify(result, null, 2) + "\n");
process.exitCode = result.pass ? 0 : 1;

function valueAfter(flag) {
  const index = process.argv.indexOf(flag);
  return index === -1 ? null : process.argv[index + 1];
}
