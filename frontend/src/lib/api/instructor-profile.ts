import { authenticatedRequest } from "./http";
import { publicCatalogRequest } from "./public-request";

export type InstructorPublicationState =
  | "DRAFT"
  | "PENDING_REVIEW"
  | "PUBLISHED"
  | "CHANGES_REQUESTED"
  | "HIDDEN";

export type InstructorExpertise = {
  id: string;
  official_code?: string | null;
  title_ar: string;
  title_en: string;
};

export type InstructorProfile = {
  account_id: string;
  display_name: string;
  public_slug?: string | null;
  headline_ar: string;
  headline_en: string;
  bio_ar: string;
  bio_en: string;
  publication_state: InstructorPublicationState;
  published_snapshot?: {
    public_slug: string;
    display_name: string;
    headline_ar: string;
    headline_en: string;
    bio_ar: string;
    bio_en: string;
    expertise: InstructorExpertise[];
  } | null;
  expertise: InstructorExpertise[];
  submitted_at?: string | null;
  decided_at?: string | null;
  decision_note?: string | null;
  revision: number;
  created_at?: string;
  updated_at?: string;
};

export type InstructorProfileQueueItem = {
  account_id: string;
  display_name: string;
  public_slug?: string | null;
  publication_state: InstructorPublicationState;
  submitted_at?: string | null;
  updated_at: string;
  revision: number;
};

export type PublicInstructorProfile = {
  public_slug: string;
  display_name: string;
  headline_ar: string;
  headline_en: string;
  bio_ar: string;
  bio_en: string;
  expertise: InstructorExpertise[];
  avatar_url?: string | null;
  courses: import("./public-catalog").PublicCourse[];
};

type ProfileInput = {
  locale: "ar" | "en";
  csrf: string;
};

function requireCSRF(input: ProfileInput): void {
  if (!input.csrf) {
    throw new Error(input.locale === "ar" ? "رمز حماية الجلسة مفقود" : "Session security token is missing");
  }
}

function requireResult<T>(result: T | null, locale: "ar" | "en"): T {
  if (result === null) {
    throw new Error(locale === "ar" ? "لم يرجع الخادم نتيجة" : "The server returned an empty result");
  }
  return result;
}

export async function getInstructorProfile(locale: "ar" | "en"): Promise<InstructorProfile> {
  return requireResult(
    await authenticatedRequest<InstructorProfile>("/me/instructor-profile", "GET", locale),
    locale,
  );
}

export async function saveInstructorProfile(
  input: ProfileInput & {
    revision: number;
    publicSlug: string;
    headlineAr: string;
    headlineEn: string;
    bioAr: string;
    bioEn: string;
    expertiseIDs: string[];
  },
): Promise<InstructorProfile> {
  requireCSRF(input);
  return requireResult(
    await authenticatedRequest<InstructorProfile>(
      "/me/instructor-profile",
      "PUT",
      input.locale,
      input.csrf,
      {
        revision: input.revision,
        public_slug: input.publicSlug,
        headline_ar: input.headlineAr,
        headline_en: input.headlineEn,
        bio_ar: input.bioAr,
        bio_en: input.bioEn,
        expertise_ids: input.expertiseIDs,
      },
    ),
    input.locale,
  );
}

export async function submitInstructorProfile(
  input: ProfileInput & { revision: number },
): Promise<InstructorProfile> {
  requireCSRF(input);
  return requireResult(
    await authenticatedRequest<InstructorProfile>(
      "/me/instructor-profile/submission",
      "POST",
      input.locale,
      input.csrf,
      { revision: input.revision },
    ),
    input.locale,
  );
}

export async function listAdminInstructorProfiles(
  locale: "ar" | "en",
  state = "",
  page = 1,
  limit = 25,
): Promise<{ items: InstructorProfileQueueItem[]; page: number; limit: number; has_more: boolean }> {
  const params = new URLSearchParams();
  if (state !== "") params.set("state", state);
  params.set("page", String(page));
  params.set("limit", String(limit));
  const suffix = "?" + params.toString();
  return requireResult(
    await authenticatedRequest<{ items: InstructorProfileQueueItem[]; page: number; limit: number; has_more: boolean }>(
      "/admin/instructor-profiles" + suffix,
      "GET",
      locale,
    ),
    locale,
  );
}

export async function getAdminInstructorProfile(
  accountID: string,
  locale: "ar" | "en",
): Promise<InstructorProfile> {
  return requireResult(
    await authenticatedRequest<InstructorProfile>(
      "/admin/instructor-profiles/" + encodeURIComponent(accountID),
      "GET",
      locale,
    ),
    locale,
  );
}

export async function moderateInstructorProfile(
  input: ProfileInput & {
    accountID: string;
    action: "approve" | "request-changes" | "hide";
    revision: number;
    reason: string;
  },
): Promise<InstructorProfile> {
  requireCSRF(input);
  return requireResult(
    await authenticatedRequest<InstructorProfile>(
      "/admin/instructor-profiles/" + encodeURIComponent(input.accountID) + "/" + input.action,
      "POST",
      input.locale,
      input.csrf,
      { revision: input.revision, reason: input.reason },
    ),
    input.locale,
  );
}

export function getPublicInstructorProfile(slug: string, locale: "ar" | "en") {
  return publicCatalogRequest<PublicInstructorProfile>(
    "/instructors/" + encodeURIComponent(slug),
    locale,
  );
}
