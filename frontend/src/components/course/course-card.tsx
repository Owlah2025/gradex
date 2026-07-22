"use client";

import * as React from "react";
import Link from "next/link";
import { CheckSquare, Clock, PlayCircle } from "lucide-react";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Tag } from "@/components/ui/tag";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/lib/i18n/locale-provider";
import { cn } from "@/lib/utils";
import type { Course } from "@/lib/types";

/** CourseCard — the shared catalog card (reused by Catalog + Course Details). */
export function CourseCard({ course }: { course: Course }) {
  const { t, locale } = useLocale();

  return (
    <Card
      interactive
      className="flex flex-col overflow-hidden p-0"
    >
      <div
        className={cn(
          "flex items-start justify-between p-3.5",
          "h-[150px]",
          course.thumb,
        )}
      >
        <span
          dir="ltr"
          className="rounded-sm bg-gx-navy/40 px-2.5 py-1 font-mono text-[12.5px] font-semibold text-white"
        >
          {course.code}
        </span>
        {course.isNew ? <Badge variant="accent">{t.courses.new}</Badge> : null}
      </div>

      <div className="flex flex-1 flex-col gap-3 p-5">
        <div className="flex flex-wrap gap-2">
          <Tag>{t.courses.levels[course.level]}</Tag>
          {course.labsIncluded ? (
            <Tag>
              <CheckSquare aria-hidden />
              {t.courses.labsIncluded}
            </Tag>
          ) : null}
        </div>

        <h3 className="font-display text-lg font-bold leading-snug text-foreground">
          {course.title[locale]}
        </h3>

        <div className="flex items-center gap-2.5 text-sm text-muted-foreground">
          <Avatar size="sm" aria-hidden>
            <AvatarFallback>{course.instructorInitial}</AvatarFallback>
          </Avatar>
          <span>{course.instructor[locale]}</span>
        </div>

        <div className="flex flex-wrap gap-4 text-[13.5px] text-muted-foreground">
          <span className="inline-flex items-center gap-1.5">
            <PlayCircle className="size-4" aria-hidden />
            <span dir="ltr">
              {course.lessons} {t.courses.lessons}
            </span>
          </span>
          <span className="inline-flex items-center gap-1.5">
            <Clock className="size-4" aria-hidden />
            <span dir="ltr">{course.duration}</span>
          </span>
        </div>

        <div className="mt-auto flex items-center justify-between border-t border-border pt-3.5">
          <span dir="ltr" className="font-mono text-[17px] font-semibold text-foreground">
            {course.price}
          </span>
          <Button asChild variant="ghost" size="sm">
            <Link href={`/courses/${course.slug}`}>{t.courses.view}</Link>
          </Button>
        </div>
      </div>
    </Card>
  );
}
