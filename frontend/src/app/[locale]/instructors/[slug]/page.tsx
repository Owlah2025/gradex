import type { Metadata } from "next";
import { PublicInstructorPage } from "@/components/instructor/public-instructor-page";

export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string; slug: string }>;
}): Promise<Metadata> {
  const { locale: requestedLocale, slug } = await params;
  const locale = requestedLocale === "en" ? "en" : "ar";
  return {
    title: locale === "ar" ? "ملف المحاضر | GradeX" : "Instructor profile | GradeX",
    description:
      locale === "ar"
        ? "تعرّف على المحاضر ومقرراته المنشورة على GradeX."
        : "Meet the instructor and explore their published GradeX courses.",
    alternates: {
      canonical: "/" + locale + "/instructors/" + slug,
    },
  };
}

export default async function PublicInstructorRoute({
  params,
}: {
  params: Promise<{ locale: string; slug: string }>;
}) {
  const { locale: requestedLocale, slug } = await params;
  const locale = requestedLocale === "en" ? "en" : "ar";
  return <PublicInstructorPage slug={slug} routeLocale={locale} />;
}
