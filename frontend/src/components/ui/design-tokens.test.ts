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

/* ---------------------------------------------------------------- success */

/**
 * The success token, which is two tokens because one could not do both jobs.
 *
 * `gx-success` (#178a50) was painted as small bold text on three separate surfaces and measured
 * 4.39:1 on white and 3.94:1 on its own soft ground — under the 4.5:1 AA minimum for normal text.
 * Three surfaces had already worked around it by reaching for a different colour, which is the
 * shape a shared-token defect takes when it is routed around rather than fixed.
 *
 * Darkening the single token to clear AA takes it to 2.87:1 against the dark theme's card, under
 * the 3:1 WCAG minimum for icons and other non-text. The two requirements pull in opposite
 * directions, so the token was split by the job rather than moved: `success` is the identity, on
 * icons and borders, and answers to 3:1 in both themes; `success-strong` is success as text on a
 * light ground, and answers to 4.5:1.
 *
 * Both live as hex in the Tailwind config rather than as HSL custom properties, so they are read
 * from there.
 */
function brandHex(name: string): [number, number, number] {
  const root = process.cwd().endsWith("/frontend")
    ? process.cwd()
    : path.join(process.cwd(), "frontend");
  const config = fs.readFileSync(path.join(root, "tailwind.config.ts"), "utf8");
  const match = config.match(
    new RegExp(`["']?${name}["']?:\\s*["']#([0-9a-fA-F]{6})["']`),
  );
  assert.ok(match, `the brand ramp has no ${name}`);
  const hex = match![1];
  return [
    parseInt(hex.slice(0, 2), 16),
    parseInt(hex.slice(2, 4), 16),
    parseInt(hex.slice(4, 6), 16),
  ];
}

/** A theme surface, read from whichever block actually defines it. */
function themeToken(
  css: string,
  name: string,
  theme: "light" | "dark",
): [number, number, number] {
  const block =
    theme === "light"
      ? css.slice(css.indexOf(":root"), css.indexOf(".dark"))
      : css.slice(css.indexOf(".dark"));
  const match = block.match(
    new RegExp(`--${name}:\\s*([\\d.]+)\\s+([\\d.]+)%\\s+([\\d.]+)%`),
  );
  assert.ok(match, `--${name} was not found in the ${theme} theme`);
  return hslToRgb([Number(match![1]), Number(match![2]) / 100, Number(match![3]) / 100]);
}

test("success as text meets AA on every light ground it is painted on", () => {
  const css = stylesheet();
  const strong = brandHex("success-strong");
  const grounds: [string, [number, number, number]][] = [
    ["the soft success ground it is paired with", brandHex("success-soft")],
    ["a card", themeToken(css, "card", "light")],
    ["the page", themeToken(css, "background", "light")],
  ];
  for (const [where, ground] of grounds) {
    const measured = contrast(strong, ground);
    assert.ok(
      measured >= 4.5,
      `gx-success-strong on ${where} is ${measured.toFixed(2)}:1, under the 4.5:1 AA minimum`,
    );
  }
});

/**
 * And the identity keeps its own, weaker but genuinely different, obligation. This is the half that
 * a single darkened token would have broken silently: an icon on the dark theme's card.
 */
test("the success identity clears the non-text minimum in both themes", () => {
  const css = stylesheet();
  const identity = brandHex("success");
  for (const theme of ["light", "dark"] as const) {
    for (const surface of ["card", "background"] as const) {
      const measured = contrast(identity, themeToken(css, surface, theme));
      assert.ok(
        measured >= 3,
        `gx-success on the ${theme} --${surface} is ${measured.toFixed(2)}:1, ` +
          "under the 3:1 WCAG minimum for non-text",
      );
    }
  }
});

/** Success must still read as success, not as ink that happens to pass. */
test("success stays green and stays distinguishable from body text", () => {
  const css = stylesheet();
  const [r, g, b] = brandHex("success-strong");
  assert.ok(g > r * 2 && g > b, `gx-success-strong is rgb(${r}, ${g}, ${b}), which is not green`);
  const onCard = contrast(brandHex("success-strong"), themeToken(css, "card", "light"));
  const bodyOnCard = contrast(
    themeToken(css, "foreground", "light"),
    themeToken(css, "card", "light"),
  );
  assert.ok(
    bodyOnCard > onCard,
    `success text is ${onCard.toFixed(2)}:1 against body text's ${bodyOnCard.toFixed(2)}:1; ` +
      "a status colour must not outweigh the prose around it",
  );
});

/**
 * The split only holds if each token stays on its own side of it.
 *
 * `success-strong` is proved above against light grounds only, so a usage that put it on the dark
 * theme's card would be painting an unproven pair. Every usage today pairs it with `success-soft`,
 * which is a fixed light ground in both themes; this keeps it that way.
 */
test("success text is only ever painted on the light ground it was proved against", () => {
  const root = process.cwd().endsWith("/frontend")
    ? process.cwd()
    : path.join(process.cwd(), "frontend");
  const offenders: string[] = [];
  const walk = (dir: string) => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        walk(full);
        continue;
      }
      if (!/\.tsx?$/.test(entry.name) || entry.name.includes(".test.")) continue;
      const source = fs.readFileSync(full, "utf8");
      for (const line of source.split("\n")) {
        if (!line.includes("text-gx-success-strong")) continue;
        if (line.includes("bg-gx-success-soft")) continue;
        offenders.push(`${path.relative(root, full)}: ${line.trim()}`);
      }
    }
  };
  walk(path.join(root, "src"));
  assert.deepEqual(
    offenders,
    [],
    "success text appears without its proved ground:\n" + offenders.join("\n"),
  );
});
