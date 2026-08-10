import fs from "fs";
import { E2E_TMP_DIR, verifyProcessOwnership } from "../../src/lib/api/e2e-infrastructure";

export const WORKER_BINARY_PATH = `${E2E_TMP_DIR}/gradex-media-e2e-worker`;
export const WORKER_STATE_FILE_PATH = `${E2E_TMP_DIR}/gradex-media-e2e-worker-state.json`;

/**
 * The complete backend environment for the media authoring run.
 *
 * Every value here is a throwaway development fixture for a per-run database
 * and the developer Compose MinIO. None of them is a production default: the
 * API refuses `MEDIA_SCANNER_MODE=DEVELOPMENT_NO_OP` unless APP_ENV is
 * development, and refuses these placeholder secrets in production outright.
 */
export function mediaStackEnvironment(options: {
  databaseURL: string;
  publicOrigin: string;
  limiterKey: string;
}): Record<string, string> {
  return {
    DATABASE_URL: options.databaseURL,
    APP_ENV: "development",
    REDIS_ADDR: process.env.GRADEX_E2E_REDIS_ADDR || "localhost:6379",

    // Real object storage. The browser PUTs to it directly and the API
    // re-reads the exact stored object version to verify the upload.
    S3_ENDPOINT: process.env.GRADEX_E2E_S3_ENDPOINT || "http://localhost:9000",
    S3_BUCKET: process.env.GRADEX_E2E_S3_BUCKET || "gradex-video",
    S3_ACCESS_KEY: process.env.GRADEX_E2E_S3_ACCESS_KEY || "gradexminio",
    S3_SECRET_KEY: process.env.GRADEX_E2E_S3_SECRET_KEY || "gradexminio",
    S3_USE_PATH_STYLE: "true",

    MEDIA_OPERATING_MODE: "SCANNER",
    MEDIA_SCANNER_MODE: "DEVELOPMENT_NO_OP",
    MEDIA_PROCESSING_TIMEOUT: "5m",
    UPLOAD_URL_EXPIRY: "15m",

    AUTH_FAKE_MODE: "false",
    STUDENT_REGISTRATION_ENABLED: "true",
    REGISTRATION_POLICY_SET_ID: "media-e2e-policy-v1",
    PASSWORD_SCREEN_MODE: "deterministic",

    // 32-byte development fixture keys for a throwaway per-run database.
    SESSION_CSRF_KEY: "0123456789abcdef0123456789abcdef",
    ANONYMOUS_COOKIE_SIGNING_KEY: "1123456789abcdef0123456789abcdef",
    ANONYMOUS_CSRF_KEY: "2123456789abcdef0123456789abcdef",
    PLAYBACK_TOKEN_SECRET: "3123456789abcdef0123456789abcdef",
    OUTBOX_PROTECTED_PAYLOAD_KEY: "4123456789abcdef0123456789abcdef",
    OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION: "media-e2e-v1",
    ADMISSION_LIMITER_HMAC_KEY: options.limiterKey,

    PUBLIC_ORIGIN: options.publicOrigin,
    CORS_ALLOWED_ORIGINS: options.publicOrigin,
    CORS_ALLOW_CREDENTIALS: "true",
  };
}

/** Terminates the run-owned worker, and only a process this run recorded. */
export function terminateWorker(): void {
  if (!fs.existsSync(WORKER_STATE_FILE_PATH)) return;
  try {
    const state = JSON.parse(fs.readFileSync(WORKER_STATE_FILE_PATH, "utf-8")) as { pid?: number };
    if (state.pid && verifyProcessOwnership(state.pid, WORKER_BINARY_PATH)) {
      process.kill(state.pid, "SIGTERM");
    }
  } catch {
    // A worker that is already gone needs no signal.
  }
  try {
    fs.unlinkSync(WORKER_STATE_FILE_PATH);
  } catch {}
}
