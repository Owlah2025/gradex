import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

/**
 * The one design-token guarantee worth asserting rather than commenting.
 *
 * `--muted-foreground` is the colour of every secondary line in the product — section leads, field
 * hints, empty-state descriptions, table column headers. It was measured at 4.48:1 against the page
 * background and 4.2:1 against the muted section tone, under WCAG AA both ways, and darkened one
 * point to fix it.
 *
 * A contrast ratio is arithmetic on two numbers that live in one file, so it can be proved here
 * without a browser and cannot silently regress the next time someone reaches for a lighter grey.
 * Deliberately narrow: this asserts the one token that was changed against the two surfaces it is
 * actually painted on, and is not an audit of the palette.
 */

function stylesheet(): string {
  const root = process.cwd().endsWith("/frontend")
    ? process.cwd()
    : path.join(process.cwd(), "frontend");
  return fs.readFileSync(path.join(root, "src/app/globals.css"), "utf8");
}

/**
 * Reads a token from the `:root` block specifically.
 *
 * Scoped on purpose: the same names are redeclared under the dark theme further down the file, and
 * a naive first-match would silently measure the wrong pair.
 */
function lightToken(css: string, name: string): [number, number, number] {
  const root = css.slice(css.indexOf(":root"), css.indexOf(".dark"));
  const match = root.match(
    new RegExp(`--${name}:\\s*([\\d.]+)\\s+([\\d.]+)%\\s+([\\d.]+)%`),
  );
  assert.ok(match, `--${name} was not found in the light theme`);
  return [Number(match![1]), Number(match![2]) / 100, Number(match![3]) / 100];
}

function hslToRgb([h, s, l]: [number, number, number]): [number, number, number] {
  const c = (1 - Math.abs(2 * l - 1)) * s;
  const x = c * (1 - Math.abs(((h / 60) % 2) - 1));
  const m = l - c / 2;
  const [r, g, b] =
    h < 60
      ? [c, x, 0]
      : h < 120
        ? [x, c, 0]
        : h < 180
          ? [0, c, x]
          : h < 240
            ? [0, x, c]
            : h < 300
              ? [x, 0, c]
              : [c, 0, x];
  return [(r + m) * 255, (g + m) * 255, (b + m) * 255];
}

/** WCAG relative luminance, then the WCAG contrast ratio. */
function luminance(rgb: [number, number, number]): number {
  const [r, g, b] = rgb.map((channel) => {
    const value = channel / 255;
    return value <= 0.03928
      ? value / 12.92
      : Math.pow((value + 0.055) / 1.055, 2.4);
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function contrast(a: [number, number, number], b: [number, number, number]): number {
  const [high, low] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (high + 0.05) / (low + 0.05);
}

function ratio(css: string, foreground: string, background: string): number {
  return contrast(
    hslToRgb(lightToken(css, foreground)),
    hslToRgb(lightToken(css, background)),
  );
}

test("secondary text meets AA on both surfaces it is painted on", () => {
  const css = stylesheet();
  for (const surface of ["background", "card"]) {
    const measured = ratio(css, "muted-foreground", surface);
    assert.ok(
      measured >= 4.5,
      `--muted-foreground on --${surface} is ${measured.toFixed(2)}:1, under the 4.5:1 AA minimum`,
    );
  }
});

/**
 * Contrast is only half of it. A secondary colour that passes AA by approaching the primary one has
 * fixed a contrast failure by destroying the type hierarchy, which is the regression this token
 * change had to be checked for and the reason the correction was one point rather than several.
 */
test("secondary text stays clearly subordinate to primary text", () => {
  const css = stylesheet();
  const secondary = ratio(css, "muted-foreground", "background");
  const primary = ratio(css, "foreground", "background");
  assert.ok(
    primary > secondary * 2,
    `primary text is ${primary.toFixed(2)}:1 against secondary's ${secondary.toFixed(2)}:1; ` +
      "secondary is no longer visibly subordinate",
  );
});
