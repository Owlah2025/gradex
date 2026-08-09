import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDirectory = path.dirname(fileURLToPath(import.meta.url));
const frontendRoot = path.resolve(scriptDirectory, "..");
const sourcePath = path.resolve(frontendRoot, "../docs/legal/lg011-approved-policy-package.md");
const outputPath = path.resolve(frontendRoot, "src/lib/legal/lg011-policy.generated.ts");

const markers = [
  ["englishPrivacy", "# English Privacy Policy", "# سياسة الخصوصية بالعربية"],
  ["arabicPrivacy", "# سياسة الخصوصية بالعربية", "# English Terms of Use"],
  ["englishTerms", "# English Terms of Use", "# شروط الاستخدام بالعربية"],
  [
    "arabicTerms",
    "# شروط الاستخدام بالعربية",
    "# Product Owner authority — Refund Policy and course-access disclosures",
  ],
];

function extract(source, startMarker, endMarker) {
  const start = source.indexOf(startMarker);
  const end = source.indexOf(endMarker, start + startMarker.length);
  if (start < 0 || end < 0 || end <= start) {
    throw new Error(`approved policy section markers are missing: ${startMarker}`);
  }
  return source.slice(start, end).trimEnd();
}

function generatedSource(source) {
  const documents = Object.fromEntries(
    markers.map(([key, start, end]) => [key, extract(source, start, end)]),
  );
  return [
    "// Generated from docs/legal/lg011-approved-policy-package.md.",
    "// Run `npm run legal:generate` from frontend after an approved source change.",
    `export const approvedPolicyDocuments = ${JSON.stringify(documents, null, 2)} as const;`,
    "",
  ].join("\n");
}

const expected = generatedSource(fs.readFileSync(sourcePath, "utf8"));
if (process.argv.includes("--check")) {
  const actual = fs.existsSync(outputPath) ? fs.readFileSync(outputPath, "utf8") : "";
  if (actual !== expected) {
    console.error("generated LG-011 policy content is missing or stale");
    process.exitCode = 1;
  }
} else {
  fs.mkdirSync(path.dirname(outputPath), { recursive: true });
  fs.writeFileSync(outputPath, expected);
}
