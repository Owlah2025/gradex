export function bundleDetailHref(locale: "ar" | "en", slugOrID: string): string {
  return `/${locale}/catalog/bundles/${encodeURIComponent(slugOrID)}`;
}

// The copy itself lives in the dictionaries, like every other localized string.
// This only fills the template so the card and the Admin workspace cannot drift
// into two different renderings of the same label.
export function bundleCourseCount(count: number, template: string): string {
  return template.replace("{count}", String(count));
}

/**
 * How many member Courses one Bundle card puts on screen.
 *
 * A Bundle may legitimately contain many Courses. Drawing all of them turns the
 * artwork into an unreadable smear of slivers, so the card shows a bounded
 * stack and states the remainder honestly with a `+N`. The full membership is
 * never hidden: the Bundle detail page lists every Course.
 */
export const BUNDLE_STACK_LIMIT = 4;

/**
 * The fraction of its own width each card behind the front one reveals.
 *
 * Every offset and every card width below is derived from this single number
 * and the visible member count, which is the point: nothing in the layout
 * assumes a particular member count, so a 2-member Bundle is a deliberate
 * 2-card layout rather than a 3-card layout with a hole in it.
 */
const REVEAL = 0.62;

export type BundleStackSlot = {
  /** Index into the member array. Identity, never a slot number. */
  index: number;
  /** 0 for the active member, growing with visual distance behind it. */
  depth: number;
  isActive: boolean;
  /** Percentage of the stack's width this card occupies. */
  widthPercent: number;
  /**
   * Logical inline start, in percent. Logical, not left/right: RTL mirrors the
   * visual offsets through `inset-inline-start` while the member order and the
   * member identities stay exactly as the server sent them.
   */
  insetInlineStartPercent: number;
  /** Active is highest, always, for any member count. */
  zIndex: number;
  scale: number;
};

export type BundleStackLayout = {
  /** Members actually drawn, front-most last in paint order is NOT assumed —
   *  z-index carries the ordering, so the array stays in member order. */
  slots: BundleStackSlot[];
  /** Members beyond the bounded stack. 0 when everything is shown. */
  overflow: number;
  /** A single Course is one piece of artwork, not a stack of one. */
  isSingle: boolean;
};

/**
 * Derives the whole stack geometry from the member count and the active member.
 *
 * Deliberately total and pure: it never reads the DOM, never looks at hover,
 * and produces a defined layout for 1, 2, 3, 4 and more members. An
 * out-of-range `activeIndex` is clamped rather than producing an empty front,
 * because a card whose front member is undefined is exactly the failure this
 * function exists to make impossible.
 */
export function bundleStackLayout(memberCount: number, activeIndex: number): BundleStackLayout {
  const total = Math.max(0, Math.floor(memberCount));
  const visible = Math.min(total, BUNDLE_STACK_LIMIT);
  if (visible === 0) return { slots: [], overflow: 0, isSingle: false };

  const active = Number.isInteger(activeIndex) && activeIndex >= 0 && activeIndex < visible ? activeIndex : 0;
  const widthPercent = 100 / (1 + (visible - 1) * REVEAL);
  const step = widthPercent * REVEAL;

  const slots: BundleStackSlot[] = [];
  for (let index = 0; index < visible; index += 1) {
    // Distance from the active member, wrapping, so the active member is always
    // at depth 0 and every other member keeps its relative order behind it.
    const depth = (index - active + visible) % visible;
    slots.push({
      index,
      depth,
      isActive: depth === 0,
      widthPercent,
      insetInlineStartPercent: depth * step,
      // The active card is above every other card for any count: there is no
      // slot table to fall out of sync with the member count.
      zIndex: visible - depth,
      scale: 1 - depth * 0.04,
    });
  }
  return { slots, overflow: total - visible, isSingle: visible === 1 };
}

/**
 * Clamps a stored active index against the member list it belongs to.
 *
 * State is keyed per Bundle and survives re-renders, so a Bundle whose
 * membership shrank underneath it must not keep pointing past the end.
 */
export function clampActiveMember(memberCount: number, activeIndex: number): number {
  const visible = Math.min(Math.max(0, Math.floor(memberCount)), BUNDLE_STACK_LIMIT);
  if (visible <= 0) return 0;
  return Number.isInteger(activeIndex) && activeIndex >= 0 && activeIndex < visible ? activeIndex : 0;
}

/** Fills the "+N more Courses" label without duplicating the template. */
export function bundleOverflowLabel(overflow: number, template: string): string {
  return template.replace("{count}", String(overflow));
}
