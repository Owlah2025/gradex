import assert from "node:assert/strict";
import test from "node:test";
import { describeApiError } from "./api-error";
import { ProblemError } from "./problem";

const mismatch = (parameter = "video/mp4") =>
  new ProblemError({
    type: "https://api.gradex.com/problems/validation-failed",
    title: "Request validation failed",
    status: 422,
    detail: "One or more fields are invalid.",
    code: "VALIDATION_FAILED",
    errors: [
      {
        code: parameter === "video/mp4" ? "CONTENT_TYPE_MISMATCH" : "VIDEO_CONTENT_TYPE_MISMATCH",
        detail: "The selected file does not match the required MP4 format.",
        location: "body",
        pointer: "/content_type",
        parameter,
      },
    ],
  });

test("content mismatch renders actionable English MP4 guidance", () => {
  assert.equal(
    describeApiError(mismatch(), "en"),
    "The selected file does not match the required MP4 format. Choose a valid MP4 video.",
  );
});

test("content mismatch renders actionable Arabic MP4 guidance", () => {
  assert.equal(
    describeApiError(mismatch(), "ar"),
    "الملف المحدد ليس بصيغة MP4 صحيحة. اختر ملف فيديو MP4 صالحًا.",
  );
});

test("non-MP4 content mismatch uses localized generic format guidance", () => {
  assert.equal(
    describeApiError(mismatch("video/quicktime"), "en"),
    "The selected file does not match the required format. Choose a valid file.",
  );
  assert.equal(
    describeApiError(mismatch("video/quicktime"), "ar"),
    "الملف المحدد لا يطابق الصيغة المطلوبة. اختر ملفًا بالصيغة الصحيحة.",
  );
});

test("unknown validation violations retain the existing generic rendering", () => {
  const error = new ProblemError({
    type: "https://api.gradex.com/problems/validation-failed",
    title: "Request validation failed",
    status: 422,
    detail: "One or more fields are invalid.",
    code: "VALIDATION_FAILED",
    errors: [{ code: "OTHER_REASON", detail: "safe detail" }],
  });
  assert.equal(describeApiError(error, "en"), "One or more fields are invalid.: OTHER_REASON · safe detail");
});
