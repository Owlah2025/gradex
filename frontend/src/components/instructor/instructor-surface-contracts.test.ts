import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";

function read(relative: string): string {
  const root = process.cwd().endsWith("/frontend") ? process.cwd() : path.join(process.cwd(), "frontend");
  return fs.readFileSync(path.join(root, "src", relative), "utf8");
}

test("instructor wire types expose announcement pagination and dashboard publication facts", () => {
  const api = read("lib/api/instructor.ts");
  assert.match(api, /export type CourseAnnouncementPage/);
  assert.match(api, /has_more: boolean/);
  assert.match(api, /published_revision\?: InstructorRevisionSummary/);
});

test("course analytics keeps metric terms valid inside the description list", () => {
  const analytics = read("components/instructor/course-analytics.tsx");
  assert.match(analytics, /function Metric[\s\S]*<div className=/);
  assert.match(analytics, /function Metric[\s\S]*<dt[\s\S]*<\/dt>[\s\S]*<dd[\s\S]*<\/dd>/);
});

test("announcement publishing keeps list failures separate from publish failures", () => {
  const announcements = read("components/instructor/course-announcements.tsx");
  assert.match(announcements, /loadError/);
  assert.match(announcements, /publishError/);
  assert.match(announcements, /aria-invalid=/);
  assert.match(announcements, /has_more/);
});
