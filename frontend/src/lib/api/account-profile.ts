import { authenticatedRequest } from "./http";

export type MyAcademicProfile = {
  institution?: string;
  college_unit?: string;
  program?: string;
  level?: number;
};

export type MyProfile = {
  display_name: string;
  email: string;
  locale: "ar" | "en";
  status: "PENDING_VERIFICATION" | "ACTIVE" | "SUSPENDED";
  academic_profile?: MyAcademicProfile;
  /** E.164, or null when the Student has not provided one. "Provided", never "verified". */
  whatsapp_number: string | null;
  whatsapp_number_updated_at?: string | null;
};

export async function getMyProfile(locale: "ar" | "en"): Promise<MyProfile> {
  const profile = await authenticatedRequest<MyProfile>("/me/profile", "GET", locale);
  if (!profile) throw new Error(locale === "ar" ? "لم يرجع الخادم ملفك" : "The server returned an empty profile");
  return profile;
}

export async function updateMyProfile(input: {
  locale: "ar" | "en";
  csrf: string;
  displayName?: string;
  nextLocale?: "ar" | "en";
  /** Replaces the Student's WhatsApp number. The server validates and normalizes it. */
  whatsappNumber?: string;
}): Promise<MyProfile> {
  if (!input.csrf) {
    throw new Error(input.locale === "ar" ? "انتهت الجلسة" : "Your session ended");
  }
  const profile = await authenticatedRequest<MyProfile>(
    "/me/profile",
    "PUT",
    input.locale,
    input.csrf,
    {
      ...(input.displayName !== undefined ? { display_name: input.displayName } : {}),
      ...(input.nextLocale !== undefined ? { locale: input.nextLocale } : {}),
      ...(input.whatsappNumber !== undefined ? { whatsapp_number: input.whatsappNumber } : {}),
    },
  );
  if (!profile) throw new Error(input.locale === "ar" ? "لم يرجع الخادم ملفك" : "The server returned an empty profile");
  return profile;
}
