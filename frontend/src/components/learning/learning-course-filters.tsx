"use client";

import * as React from "react";
import { Button } from "@/components/ui/button";
import {
  initialLearningCourseFilter,
  matchesLearningCourseFilter,
  type LearningCourseFilter,
  type LearningCourseFilterCard,
} from "./learning-course-filter-model";

export type LearningCourseFilterLabels = {
  filterLabel: string;
  inProgress: string;
  completed: string;
  all: string;
  empty: string;
};

export function LearningCourseFilters({
  cards,
  labels,
}: {
  cards: Array<LearningCourseFilterCard & { key: string; content: React.ReactNode }>;
  labels: LearningCourseFilterLabels;
}) {
  const [filter, setFilter] = React.useState<LearningCourseFilter>(() =>
    initialLearningCourseFilter(cards),
  );
  const filters: Array<{ value: LearningCourseFilter; label: string }> = [
    { value: "in-progress", label: labels.inProgress },
    { value: "completed", label: labels.completed },
    { value: "all", label: labels.all },
  ];
  const visible = cards.filter((card) => matchesLearningCourseFilter(card, filter));

  return (
    <div>
      <div
        className="flex max-w-full gap-1 overflow-x-auto border-b border-border"
        role="group"
        aria-label={labels.filterLabel}
      >
        {filters.map((filterOption) => (
          <Button
            key={filterOption.value}
            type="button"
            aria-pressed={filter === filterOption.value}
            variant={filter === filterOption.value ? "secondary" : "ghost"}
            className="min-h-11 shrink-0 rounded-b-none"
            onClick={() => setFilter(filterOption.value)}
          >
            {filterOption.label}
          </Button>
        ))}
      </div>
      {visible.length === 0 ? (
        <p className="mt-6 rounded-lg border border-dashed border-border px-4 py-8 text-center text-sm text-muted-foreground">
          {labels.empty}
        </p>
      ) : (
        <ul className="mt-5 grid gap-4 md:grid-cols-2">
          {visible.map((card) => (
            <li key={card.key}>{card.content}</li>
          ))}
        </ul>
      )}
    </div>
  );
}
