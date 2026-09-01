import * as React from "react";
import Link from "next/link";
import { ChevronLeft, ChevronRight } from "lucide-react";

export type Crumb = {
  label: string;
  /** Omitted on the last crumb: the page you are on is not a link to itself. */
  href?: string;
};

/**
 * Where this page sits, and the way back up.
 *
 * Course Details and a Lesson are both two or three levels down a hierarchy the
 * visitor navigated into, and neither said so. The only route back up was the
 * browser's own Back button, which is not navigation the page provides — it
 * fails for anyone who arrived by a shared link, and it says nothing about
 * where "up" even is.
 *
 * Every level is a real `<a>`. History-based controls cannot be opened in a new
 * tab, cannot be bookmarked, and go somewhere different depending on how the
 * reader arrived; a link goes to the same place every time.
 *
 * The separator flips with the writing direction, and is `aria-hidden` because
 * the list structure already conveys the nesting to a screen reader.
 */
export function Breadcrumbs({
  items,
  label,
  locale,
  className,
}: {
  items: Crumb[];
  /** The accessible name of the navigation region, e.g. "Breadcrumb". */
  label: string;
  locale: "ar" | "en";
  className?: string;
}) {
  // A single crumb is the page itself and describes no hierarchy at all.
  if (items.length < 2) return null;
  const Separator = locale === "ar" ? ChevronLeft : ChevronRight;

  return (
    <nav
      aria-label={label}
      data-testid="breadcrumbs"
      className={className ?? "mb-4"}
    >
      {/* Each crumb is a full 24px target and the row is spaced accordingly.
          A breadcrumb is a row of small adjacent links, which is exactly the
          shape WCAG's target-size rule exists for: bare text links a few
          pixels apart read as one another's obstruction. */}
      <ol className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm text-muted-foreground">
        {items.map((item, index) => {
          const last = index === items.length - 1;
          return (
            <li
              key={`${item.href ?? "current"}-${index}`}
              className="flex min-h-6 min-w-0 items-center gap-2"
            >
              {index > 0 ? (
                <Separator className="size-4 shrink-0 opacity-60" aria-hidden />
              ) : null}
              {item.href && !last ? (
                <Link
                  href={item.href}
                  className="inline-flex min-h-6 items-center rounded-sm px-1 underline-offset-4 hover:text-foreground hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  {item.label}
                </Link>
              ) : (
                // The current page is announced as such rather than being a
                // link that does nothing when followed.
                <span
                  aria-current="page"
                  className="inline-flex min-h-6 min-w-0 items-center truncate px-1 font-semibold text-foreground"
                >
                  {item.label}
                </span>
              )}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}
