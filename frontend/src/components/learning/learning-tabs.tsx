"use client";

import * as React from "react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

/**
 * The Lesson's secondary reading, under the player and its navigation.
 *
 * # WHAT IS AND IS NOT HERE
 *
 * Three tabs, and only three, because three are all this product actually has: what the Lesson's
 * access and progress amount to, the files it carries, and the way to report it. There is no Q&A,
 * no notes and no announcements in Gradex — no table, no route, no handler — so there is no tab for
 * them. An empty tab labelled with a feature that does not exist is a promise the product cannot
 * keep, and is worse than the absence it is trying to hide.
 *
 * # WHY THE PANELS ARE PASSED IN
 *
 * Every panel is composed on the server and handed over already built. This component chooses which
 * one is visible and nothing else: it never sees a read model, a label catalogue, a download path
 * or a report context. That keeps the Lesson's payload rule intact — a component receives the
 * narrowest thing it renders — while the disclosure itself stays client-side, where it has to be.
 *
 * Panels are kept mounted (`forceMount` is deliberately *not* used, but Radix unmounts inactive
 * panels by default). Unmounting is what we want: a report dialog left half-filled in a hidden
 * panel is state a Student cannot see and cannot dismiss.
 *
 * # DIRECTION IS A REQUIRED INPUT
 *
 * `dir` is not optional and is not inferred. Radix resolves tab direction through
 * `useDirection`, which is `localDir || context || "ltr"` and reads no DOM — so an Arabic reader
 * given nothing gets LTR roving focus, and every arrow key moves opposite to the tabs they can
 * see. Making it a required prop is what stops that being re-introduced by a caller who forgets.
 */
export function LearningTabs({
  label,
  dir,
  tabs,
  className,
}: {
  /** The tab strip's accessible name, so a screen reader can say what the tabs are for. */
  label: string;
  /** The reading direction, passed on to Radix because it cannot work it out for itself. */
  dir: "ltr" | "rtl";
  tabs: Array<{ value: string; label: string; content: React.ReactNode }>;
  className?: string;
}) {
  const available = tabs.filter((tab) => tab.content !== null && tab.content !== false);
  if (available.length === 0) return null;
  return (
    <Tabs
      defaultValue={available[0].value}
      dir={dir}
      className={className}
      data-testid="learning-tabs"
    >
      <TabsList aria-label={label}>
        {available.map((tab) => (
          <TabsTrigger key={tab.value} value={tab.value} data-testid={`learning-tab-${tab.value}`}>
            {tab.label}
          </TabsTrigger>
        ))}
      </TabsList>
      {available.map((tab) => (
        <TabsContent
          key={tab.value}
          value={tab.value}
          data-testid={`learning-panel-${tab.value}`}
        >
          {tab.content}
        </TabsContent>
      ))}
    </Tabs>
  );
}
