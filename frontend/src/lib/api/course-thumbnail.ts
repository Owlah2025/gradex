import { authenticatedRequest } from "./http";
import type { CourseRevisionWire } from "./catalog";
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
  const ticket = await beginUpload({ ...input, kind: "THUMBNAIL", contentType: input.file.type, sizeBytes: input.file.size });
  const sha256 = await sha256Hex(input.file);
  const stored = await uploadFileToStorage(ticket.upload_url, input.file, input.file.type, input.onProgress);
  input.onProcessing();
  const completion = { ...input, assetVersionID: ticket.asset_version_id, providerEventID: newProviderEventID(),
    storageObjectKey: ticket.storage_object_key, storageObjectVersion: stored.storageObjectVersion,
    contentType: input.file.type, sizeBytes: input.file.size, sha256 };
  // The same receipt and compare-and-set are safe after an ambiguous response.
  const finish = async () => {
    await completeUpload(completion);
    await setCourseThumbnail({ ...input, assetID: ticket.asset_version_id });
  };
  try { await finish(); } catch (error) {
    if (error instanceof ProblemError && error.problem.status < 500) throw error;
    await finish();
  }
}
