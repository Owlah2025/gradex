export type AnnouncementField = "title" | "body";
export type AnnouncementValidationCode = "REQUIRED" | "TOO_LONG";
export type AnnouncementValidationErrors = Partial<
  Record<AnnouncementField, AnnouncementValidationCode>
>;

const limits: Record<AnnouncementField, number> = { title: 140, body: 4000 };

export function validateAnnouncementDraft(title: string, body: string): AnnouncementValidationErrors {
  const values: Record<AnnouncementField, string> = { title: title.trim(), body: body.trim() };
  const errors: AnnouncementValidationErrors = {};
  for (const field of ["title", "body"] as const) {
    if (values[field].length === 0) errors[field] = "REQUIRED";
    else if (Array.from(values[field]).length > limits[field]) errors[field] = "TOO_LONG";
  }
  return errors;
}

export function announcementFieldLength(value: string): number {
  return Array.from(value).length;
}
