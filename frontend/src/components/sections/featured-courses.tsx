"use client";

import * as React from "react";
import Link from "next/link";
import { GraduationCap } from "lucide-react";
import { Section, SectionHeader } from "@/components/layout/section";
import { CourseCard } from "@/components/course/course-card";
import { Reveal } from "@/components/common/reveal";
import { EmptyState } from "@/components/common/empty-state";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/lib/i18n/locale-provider";
import { routes } from "@/components/layout/nav-items";
import { featuredCourses } from "@/data/courses";

export function FeaturedCourses() {
  const { t } = useLocale();
  const courses = featuredCourses;

  return (
    <Section id="courses" aria-labelledby="courses-title">
      <SectionHeader
        eyebrow={t.courses.eyebrow}
        title={t.courses.title}
        lead={t.courses.subtitle}
        headingId="courses-title"
      />

      {courses.length === 0 ? (
        <EmptyState
          icon={<GraduationCap aria-hidden />}
          title={t.courses.emptyTitle}
          description={t.courses.emptyBody}
          action={
            <Button asChild>
              <Link href={routes.register}>{t.courses.emptyAction}</Link>
            </Button>
          }
        />
      ) : (
        <>
          <ul className="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
            {courses.map((course, i) => (
              <Reveal as="li" key={course.slug} delay={(i % 3) as 0 | 1 | 2}>
                <CourseCard course={course} />
              </Reveal>
            ))}
          </ul>
          <div className="mt-9 flex justify-center">
            <Button asChild variant="outline">
              <Link href={routes.courses}>{t.courses.browseAll}</Link>
            </Button>
          </div>
        </>
      )}
    </Section>
  );
}
