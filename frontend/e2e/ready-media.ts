import { execFileSync } from "child_process";
import fs from "fs";
import { e2eDatabaseEnvironment, RUN_STATE_FILE_PATH, SEED_BINARY_PATH } from "../src/lib/api/e2e-infrastructure";

type ReadyVideoFixture = { asset_version_id: string };

/**
 * Creates a disposable READY video evidence row bound to a browser-created Course.
 *
 * The production authoring endpoint deliberately rejects an Asset Version owned by another
 * Course. Dynamic browser journeys therefore cannot reuse the static fixture asset. The seeder
 * helper writes only the run-owned database rows and returns the resulting ID; all attachment and
 * authorization behavior still travels through the real API.
 */
export function seedReadyVideoForCourse(courseID: string, ownerAccountID: string): string {
  if (!fs.existsSync(RUN_STATE_FILE_PATH)) {
    throw new Error(`E2E run state is missing at ${RUN_STATE_FILE_PATH}; cannot seed course media.`);
  }
  const state = JSON.parse(fs.readFileSync(RUN_STATE_FILE_PATH, "utf-8")) as { dbName?: string };
  if (typeof state.dbName !== "string" || state.dbName === "") {
    throw new Error("E2E run state has no isolated database name; refusing to seed course media.");
  }

  const output = execFileSync(
    SEED_BINARY_PATH,
    [
      "-seed-ready-video-course",
      courseID,
      "-seed-ready-video-owner",
      ownerAccountID,
    ],
    {
      env: { ...process.env, ...e2eDatabaseEnvironment(state.dbName) },
      encoding: "utf-8",
    },
  );
  let fixture: ReadyVideoFixture;
  try {
    fixture = JSON.parse(output.trim()) as ReadyVideoFixture;
  } catch {
    throw new Error("The E2E media seeder returned non-JSON output.");
  }
  if (!fixture.asset_version_id || !/^[0-9a-f-]{36}$/i.test(fixture.asset_version_id)) {
    throw new Error("The E2E media seeder returned no valid Asset Version ID.");
  }
  return fixture.asset_version_id;
}
