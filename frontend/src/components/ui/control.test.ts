import assert from "node:assert/strict";
import test from "node:test";
import { controlClasses } from "./control";

/**
 * The exact class list `Input` rendered before the three form controls were given a shared surface.
 * It is pinned here because the point of `control.tsx` is that `Input`, `Select` and `Textarea`
 * cannot drift apart again — and the cheapest way for that guarantee to be broken is for the
 * shared default to quietly stop matching the control every existing screen already uses.
 */
const INPUT_BEFORE_CONSOLIDATION = [
  "h-12",
  "w-full",
  "rounded-md",
  "border",
  "border-input",
  "bg-card",
  "px-4",
  "text-base",
  "text-foreground",
  "shadow-sm",
  "outline-none",
  "transition-[border-color,box-shadow]",
  "placeholder:text-muted-foreground/75",
  "focus-visible:border-primary",
  "focus-visible:ring-2",
  "focus-visible:ring-ring",
  "focus-visible:ring-offset-2",
  "disabled:cursor-not-allowed",
  "disabled:opacity-60",
];

test("the default control surface still renders exactly what Input rendered before", () => {
  const classes = new Set(controlClasses("default").split(/\s+/));
  const missing = INPUT_BEFORE_CONSOLIDATION.filter((name) => !classes.has(name));
  assert.deepEqual(missing, [], "the shared default dropped a class existing screens depend on");
});

test("every control size keeps the focus ring and the token-based surface", () => {
  for (const controlSize of ["default", "sm", "auto"] as const) {
    const classes = new Set(controlClasses(controlSize).split(/\s+/));
    assert.ok(classes.has("focus-visible:ring-2"), `${controlSize} lost its focus ring`);
    assert.ok(classes.has("border-input"), `${controlSize} stopped using the border token`);
    assert.ok(classes.has("bg-card"), `${controlSize} stopped using the surface token`);
  }
});

/**
 * 16px is the default; below 16px iOS Safari zooms the viewport when the field takes focus, which
 * on a filter row above a table leaves the reader scrolled sideways into a control they cannot see
 * the rest of. The operational density may be smaller than the default, but not smaller than this.
 */
test("the operational density does not go below the size iOS zooms at", () => {
  const classes = controlClasses("sm").split(/\s+/);
  assert.ok(classes.includes("text-sm"), "the sm control must stay at text-sm");
  assert.ok(!classes.includes("text-xs"), "text-xs on an input triggers iOS focus zoom");
});
