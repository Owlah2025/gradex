import type { SavedUploadSummary } from "../../lib/api/media-multipart";
import { PartTransferError, ResumeFileMismatchError, UploadAlreadyRunningError } from "../../lib/api/media-multipart";
import { describeApiError } from "../../lib/api/api-error";
import { formatBytes, type UploadProgress } from "../../lib/api/upload-progress";

type Locale = "ar" | "en";

/*
  Byte figures are left-to-right runs ("1.6 MB"). Inside an Arabic sentence the bidi algorithm
  otherwise splits the number from its unit ("MB / 25 MB 1.6"), so each figure is wrapped in a
  left-to-right isolate (U+2066 … U+2069). English needs no isolates.
*/
const LRI = "\u2066";
const PDI = "\u2069";
/** A file name keeps its own direction inside the other language's sentence (U+2068 … U+2069). */
export function fileNameFor(name: string, locale: Locale): string {
  return locale === "ar" ? `\u2068“${name}”${PDI}` : `“${name}”`;
}

export function bytesFor(bytes: number, locale: Locale): string {
  return locale === "ar" ? `${LRI}${formatBytes(bytes)}${PDI}` : formatBytes(bytes);
}

/**
 * Live transfer readout: "425 MB / 625 MB transferred · 8.4 MB/s · ~24 sec remaining".
 * "Transferred" includes bytes of parts still in flight; see savedForResumeLine for what is safe.
 */
export function transferLine(
  progress: Pick<UploadProgress, "reportedBytes" | "totalBytes">,
  bytesPerSecond: number | null,
  secondsRemaining: number | null,
  locale: Locale,
): string {
  const isolate = (text: string) => (locale === "ar" ? `${LRI}${text}${PDI}` : text);
  const figures = isolate(`${formatBytes(progress.reportedBytes)} / ${formatBytes(progress.totalBytes)}`);
  const parts = [locale === "ar" ? `أُرسل ${figures}` : `${figures} transferred`];
  if (bytesPerSecond !== null && bytesPerSecond > 0) parts.push(isolate(`${formatBytes(bytesPerSecond)}/s`));
  if (secondsRemaining !== null && progress.reportedBytes < progress.totalBytes)
    parts.push(remainingLabel(secondsRemaining, locale));
  return parts.join(" · ");
}

export function remainingLabel(seconds: number, locale: Locale): string {
  const ar = locale === "ar";
  if (seconds < 60) {
    const value = Math.max(1, Math.round(seconds));
    return ar ? `متبقٍ نحو ${value} ثانية` : `~${value} sec remaining`;
  }
  const minutes = Math.ceil(seconds / 60);
  if (minutes < 60) return ar ? `متبقٍ نحو ${minutes} دقيقة` : `~${minutes} min remaining`;
  const hours = Math.floor(minutes / 60);
  const rest = minutes % 60;
  return ar
    ? `متبقٍ نحو ${hours} ساعة${rest ? ` و${rest} دقيقة` : ""}`
    : `~${hours} h${rest ? ` ${rest} min` : ""} remaining`;
}

/** Whole percent, rounded down: a saved figure is never overstated. */
export function durablePercent(progress: Pick<UploadProgress, "completedBytes" | "totalBytes">): number {
  if (progress.totalBytes <= 0) return 100;
  return Math.min(100, Math.floor((progress.completedBytes / progress.totalBytes) * 100));
}

/**
 * The part of the transfer that would survive a refresh, shown only when it differs from what has
 * been sent: bytes of parts the storage provider has fully accepted. In-flight bytes are never
 * called saved.
 */
export function savedForResumeLine(
  progress: Pick<UploadProgress, "completedBytes" | "transferredBytes" | "reportedBytes" | "totalBytes">,
  locale: Locale,
): string | null {
  // Visibility is decided on bytes, not on rounded percentages: 1 MB in flight on a 200 MB file is
  // under 1% and must still say that nothing is saved yet. Only the displayed figure is rounded.
  if (progress.reportedBytes <= progress.completedBytes) return null;
  const saved = durablePercent(progress);
  const inFlight = progress.transferredBytes > progress.completedBytes;
  if (progress.completedBytes <= 0)
    return locale === "ar"
      ? "لم يُحفظ شيء للاستكمال بعد. تضيع الأجزاء التي ما زالت قيد الإرسال إذا حدّثت الصفحة أو أغلقتها."
      : "Nothing is saved for resume yet. Parts still transferring are lost if you refresh or close this page.";
  const bytes = bytesFor(progress.completedBytes, locale);
  if (locale === "ar")
    return `محفوظ للاستكمال: ${saved}% · ${bytes} مرفوعة بأمان.${inFlight ? " تضيع الأجزاء التي ما زالت قيد الإرسال إذا حدّثت الصفحة أو أغلقتها." : ""}`;
  return `Saved for resume: ${saved}% · ${bytes} safely uploaded.${inFlight ? " Parts still transferring are lost if you refresh or close this page." : ""}`;
}

export function pausingLine(locale: Locale): string {
  return locale === "ar"
    ? "جارٍ إنهاء أجزاء الرفع الحالية لحفظ تقدمك…"
    : "Finishing the current upload parts so your progress can be saved…";
}

export function retryingLine(locale: Locale): string {
  return locale === "ar"
    ? "انقطع الاتصال. تجري إعادة المحاولة تلقائياً…"
    : "Connection interrupted. Retrying automatically…";
}

export function checkingLine(locale: Locale): string {
  return locale === "ar" ? "جارٍ التحقق من الملف…" : "Checking file…";
}

export function resumingLine(summary: SavedUploadSummary, locale: Locale): string {
  const ar = locale === "ar";
  if (summary.percent === null)
    return ar ? "جارٍ استكمال الرفع…" : "Resuming upload…";
  return ar ? `جارٍ الاستكمال من ${summary.percent}%…` : `Resuming from ${summary.percent}%…`;
}

/**
 * What a saved upload looks like before (or without) the file being reselected. Every number
 * comes from the summary, which the server has reconciled when it could be reached.
 */
export function savedUploadCopy(
  summary: SavedUploadSummary,
  locale: Locale,
  fileAvailable: boolean,
): { title: string; detail: string | null; instruction: string; action: string } {
  const ar = locale === "ar";
  const name = summary.fileName ? fileNameFor(summary.fileName, locale) : null;
  const action = fileAvailable
    ? ar
      ? "استكمال الرفع"
      : "Resume upload"
    : ar
      ? "اختر الملف للاستكمال"
      : "Choose file and resume";
  if (summary.status === "CANCELLED") {
    return {
      title: ar ? "انتهى هذا الرفع المحفوظ أو أُلغي" : "This saved upload was cancelled or expired",
      detail: null,
      instruction: ar
        ? "ألغِ الرفع المحفوظ لبدء رفع جديد."
        : "Cancel the saved upload to start a new one.",
      action,
    };
  }
  const detail =
    summary.sizeBytes !== null
      ? ar
        ? `تم حفظ ${bytesFor(summary.completedBytes, locale)} من ${bytesFor(summary.sizeBytes, locale)} بأمان.`
        : `${formatBytes(summary.completedBytes)} of ${formatBytes(summary.sizeBytes)} is already safely uploaded.`
      : summary.completedBytes > 0
        ? ar
          ? `تم حفظ ${bytesFor(summary.completedBytes, locale)} بأمان.`
          : `${formatBytes(summary.completedBytes)} is already safely uploaded.`
        : null;
  if (summary.status === "FINISHING") {
    return {
      title: ar ? "اكتمل رفع الملف وينتظر التحقق" : "Upload finished — verification pending",
      detail,
      instruction: fileAvailable
        ? ar
          ? "تابع لإكمال التحقق. لن يُعاد رفع أي جزء."
          : "Continue to finish verification. Nothing will be uploaded again."
        : ar
          ? `أعد اختيار ${name ?? "الملف نفسه"} لإكمال التحقق. لن يُعاد رفع أي جزء.`
          : `Reselect ${name ?? "the same file"} to finish verification. Nothing will be uploaded again.`,
      action,
    };
  }
  if (summary.percent === 100) {
    return {
      title: ar ? "توقف الرفع مؤقتاً — كل الأجزاء محفوظة" : "Upload paused — every part is saved",
      detail,
      instruction: fileAvailable
        ? ar
          ? "استكمل لإنهاء الرفع. لن يُعاد رفع أي جزء."
          : "Resume to finish. Nothing will be uploaded again."
        : ar
          ? `أعد اختيار ${name ?? "الملف نفسه"} لإنهاء الرفع. لن يُعاد رفع أي جزء.`
          : `Reselect ${name ?? "the same file"} to finish. Nothing will be uploaded again.`,
      action,
    };
  }
  const title =
    summary.percent !== null
      ? ar
        ? `توقف الرفع مؤقتاً عند ${summary.percent}%`
        : `Upload paused at ${summary.percent}%`
      : ar
        ? "توقف الرفع مؤقتاً"
        : "Upload paused";
  return {
    title,
    detail,
    instruction: fileAvailable
      ? ar
        ? "استكمل للمتابعة. لن يُعاد رفع الأجزاء المكتملة."
        : "Resume to continue. Completed parts will not be uploaded again."
      : ar
        ? `أعد اختيار ${name ?? "الملف نفسه"} للمتابعة. لن يُعاد رفع الأجزاء المكتملة.`
        : `Reselect ${name ?? "the same file"} to continue. Completed parts will not be uploaded again.`,
    action,
  };
}

export function cancelledLine(locale: Locale): string {
  return locale === "ar" ? "أُلغي الرفع." : "Upload cancelled.";
}

/**
 * The connection, not the file, ended the upload after its automatic retries. Everything stored is
 * kept and the upload resumes from it, so this is shown as an interruption, not a failure.
 */
export function isConnectionInterruption(error: unknown): boolean {
  return error instanceof PartTransferError;
}

/** Upload errors as the Instructor reads them; the wrong-file refusal names the file to pick. */
export function describeUploadError(error: unknown, locale: Locale): string {
  if (error instanceof ResumeFileMismatchError) {
    const name = error.savedFileName ? fileNameFor(error.savedFileName, locale) : null;
    return locale === "ar"
      ? `هذا ليس الملف نفسه الخاص بالرفع المتوقف. اختر ${name ?? "الملف الأصلي"} للمتابعة، أو ألغِ الرفع المحفوظ وابدأ رفعاً جديداً.`
      : `This is not the same file as the paused upload. Select ${name ?? "the original file"} to continue, or cancel the saved upload and start a new one.`;
  }
  if (error instanceof PartTransferError)
    return locale === "ar"
      ? "توقف الرفع لأن الاتصال انقطع أو أصبح بطيئاً جداً. كل ما رُفع محفوظ — استكمل الرفع للمتابعة."
      : "The upload stopped because the connection dropped or became too slow. Everything already uploaded is saved — resume to continue.";
  if (error instanceof UploadAlreadyRunningError)
    return locale === "ar"
      ? "هذا الرفع قيد التشغيل في علامة تبويب أو نافذة أخرى."
      : "This upload is already running in another tab or window.";
  return describeApiError(error, locale);
}
