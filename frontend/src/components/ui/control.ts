/**
 * The one set of form-control surfaces, shared by `Input`, `Select` and `Textarea`.
 *
 * It exists so the three controls cannot drift apart: before this, a select beside an input on the
 * same row was a different height, a different radius, a different border colour, and had no focus
 * ring at all.
 *
 * Plain strings rather than a `cva` definition, so this module has no runtime dependency and can be
 * asserted directly in the unit lane — the guarantee it makes is exactly the kind that is worth a
 * test and worthless as a comment.
 */
export type ControlSize = "default" | "sm" | "auto";

const CONTROL_BASE =
  "w-full rounded-md border border-input bg-card text-foreground shadow-sm outline-none transition-[border-color,box-shadow] placeholder:text-muted-foreground/75 focus-visible:border-primary focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-60";

/**
 * `sm` is the operational density. The default height suits a form with four fields and room to
 * breathe; a filter row above a table of forty rows does not have that room, and the previous answer
 * on those screens was a bespoke `p-2 text-xs` control rather than a smaller version of the real one.
 *
 * `text-sm` is the floor. Below 16px, iOS Safari zooms the viewport when a field takes focus, which
 * leaves the reader scrolled sideways into a control they can no longer see the rest of.
 */
const CONTROL_SIZE: Record<ControlSize, string> = {
  default: "h-12 px-4 text-base",
  sm: "h-10 px-3 text-sm",
  /** Height comes from the content — for `<textarea>`. */
  auto: "px-4 text-base",
};

export function controlClasses(controlSize: ControlSize = "default"): string {
  return `${CONTROL_BASE} ${CONTROL_SIZE[controlSize]}`;
}
