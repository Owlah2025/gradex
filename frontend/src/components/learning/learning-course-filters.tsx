"use client";

import * as React from "react";
import { Button } from "@/components/ui/button";

type Filter = "in-progress" | "completed" | "all";

export type LearningCourseFilterLabels = {
  inProgress: string;
  completed: string;
  all: string;
  empty: string;
};

export function LearningCourseFilters({
  cards,
  labels,
}: {
  cards: Array<{ key: string; completed: boolean; content: React.ReactNode }>;
  labels: LearningCourseFilterLabels;
}) {
  const [filter, setFilter] = React.useState<Filter>("in-progress");
  const tabs: Array<{ value: Filter; label: string }> = [
    { value: "in-progress", label: labels.inProgress },
    { value: "completed", label: labels.completed },
    { value: "all", label: labels.all },
  ];
  const visible = cards.filter((card) => filter === "all" || card.completed === (filter === "completed"));

  return (
    <div>
      <div className="flex max-w-full gap-1 overflow-x-auto border-b border-border" role="tablist">
        {tabs.map((tab) => (
          <Button
            key={tab.value}
            type="button"
            role="tab"
            aria-selected={filter === tab.value}
            variant={filter === tab.value ? "secondary" : "ghost"}
            className="min-h-11 shrink-0 rounded-b-none"
            onClick={() => setFilter(tab.value)}
          >
            {tab.label}
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
