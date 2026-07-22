"use client";

import * as React from "react";
import { cn } from "@/lib/utils";

/**
 * Reveal — fade + 18px rise when the element scrolls into view (design-system
 * "scroll reveal" pattern, IntersectionObserver at threshold 0.15). Honors
 * prefers-reduced-motion by rendering visible immediately. Renders semantically
 * neutral markup via `as` so it never breaks heading/list structure.
 */
export function Reveal({
  children,
  className,
  delay = 0,
  as: Tag = "div",
}: {
  children: React.ReactNode;
  className?: string;
  delay?: 0 | 1 | 2 | 3;
  as?: "div" | "li";
}) {
  const ref = React.useRef<HTMLElement | null>(null);
  const [shown, setShown] = React.useState(false);

  React.useEffect(() => {
    const el = ref.current;
    if (!el) return;

    const prefersReduced = window.matchMedia(
      "(prefers-reduced-motion: reduce)",
    ).matches;
    if (prefersReduced || !("IntersectionObserver" in window)) {
      setShown(true);
      return;
    }

    const observer = new IntersectionObserver(
      (entries) => {
        entries.forEach((entry) => {
          if (entry.isIntersecting) {
            setShown(true);
            observer.disconnect();
          }
        });
      },
      { threshold: 0.15 },
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  const delayClass = [
    "",
    "[transition-delay:70ms]",
    "[transition-delay:140ms]",
    "[transition-delay:210ms]",
  ][delay];

  return (
    <Tag
      ref={ref as React.Ref<never>}
      className={cn(
        "transition-[opacity,transform] duration-slow ease-out-brand motion-reduce:transition-none",
        shown ? "translate-y-0 opacity-100" : "translate-y-[18px] opacity-0",
        delayClass,
        className,
      )}
    >
      {children}
    </Tag>
  );
}
