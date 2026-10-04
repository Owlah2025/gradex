import { authenticatedRequest } from "./http";
import type { CourseRevisionWire } from "./catalog";
import { uploadResumable } from "./media-multipart";
import { beginUpload, completeUpload, newProviderEventID, sha256Hex, uploadFileToStorage } from "./media-upload";
import type { LocalisedInput } from "./media-upload";
import { ProblemError } from "./problem";

export const THUMBNAIL_MAX_BYTES = 5 * 1024 * 1024;
export const THUMBNAIL_TYPES = ["image/jpeg", "image/png", "image/webp"];
export type ThumbnailValidation = "type" | "size" | null;

export function validateThumbnailFile(file: Pick<File, "type" | "size">): ThumbnailValidation {
  if (!THUMBNAIL_TYPES.includes(file.type)) return "type";
  if (file.size <= 0 || file.size > THUMBNAIL_MAX_BYTES) return "size";
  return null;
}

export function revisionThumbnailURL(courseID: string, revisionID: string, assetID: string, admin = false): string {
  const prefix = admin ? "/api/v1/admin/review" : "/api/v1";
  return `${prefix}/courses/${encodeURIComponent(courseID)}/revisions/${encodeURIComponent(revisionID)}/thumbnails/${encodeURIComponent(assetID)}/card`;
}

type SelectionInput = LocalisedInput & {
  courseID: string; revisionID: string; assetID: string | null; expectedAssetID: string | null;
};

export async function setCourseThumbnail(input: SelectionInput): Promise<CourseRevisionWire> {
  const revision = await authenticatedRequest<CourseRevisionWire>(
    `/courses/${encodeURIComponent(input.courseID)}/revisions/${encodeURIComponent(input.revisionID)}/thumbnail`,
    "PUT", input.locale, input.csrf,
    { thumbnail_asset_version_id: input.assetID, expected_asset_version_id: input.expectedAssetID },
  );
  if (!revision) throw new Error("Thumbnail selection returned no revision");
  return revision;
}

export async function uploadCourseThumbnail(input: Omit<SelectionInput, "assetID"> & {
  file: File; onProgress: (progress: number) => void; onProcessing: () => void;
}): Promise<void> {
  const completionResult = await uploadResumable(input.file, {
    courseID: input.courseID,
    revisionID: input.revisionID,
    kind: "THUMBNAIL",
    storageKeyId: "thumbnail-" + input.courseID,
    locale: input.locale,
    csrf: input.csrf,
  }, input.onProgress);
  input.onProcessing();
  const finish = async () => {
    await setCourseThumbnail({ ...input, assetID: completionResult.asset_version_id });
  };
  try { await finish(); } catch (error) {
    if (error instanceof ProblemError && error.problem.status < 500) throw error;
    await finish();
  }
}
