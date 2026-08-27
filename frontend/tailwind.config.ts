import type { Config } from "tailwindcss";

/**
 * Gradex Tailwind config.
 *
 * Two colour layers, both sourced from the design system
 * (docs/design-system/tokens):
 *  - Semantic shadcn tokens (background/foreground/primary/…) as HSL channel
 *    triplets so opacity modifiers + dark mode work. Light/dark values live in
 *    globals.css.
 *  - The raw Gradex brand ramp under `gx.*` (hex) for brand-specific surfaces
 *    (gradients, chips, dark bands) where the semantic set doesn't fit.
 *
 * Never hardcode a hex in a component — reach for a token here.
 */
const config: Config = {
  darkMode: "class",
  content: [
    "./src/app/**/*.{ts,tsx}",
    "./src/components/**/*.{ts,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        border: "hsl(var(--border))",
        input: "hsl(var(--input))",
        ring: "hsl(var(--ring))",
        background: "hsl(var(--background))",
        foreground: "hsl(var(--foreground))",
        primary: {
          DEFAULT: "hsl(var(--primary))",
          foreground: "hsl(var(--primary-foreground))",
        },
        secondary: {
          DEFAULT: "hsl(var(--secondary))",
          foreground: "hsl(var(--secondary-foreground))",
        },
        destructive: {
          DEFAULT: "hsl(var(--destructive))",
          foreground: "hsl(var(--destructive-foreground))",
        },
        muted: {
          DEFAULT: "hsl(var(--muted))",
          foreground: "hsl(var(--muted-foreground))",
        },
        accent: {
          DEFAULT: "hsl(var(--accent))",
          foreground: "hsl(var(--accent-foreground))",
        },
        popover: {
          DEFAULT: "hsl(var(--popover))",
          foreground: "hsl(var(--popover-foreground))",
        },
        card: {
          DEFAULT: "hsl(var(--card))",
          foreground: "hsl(var(--card-foreground))",
        },
        // Brand ramp — raw hex from the design system.
        gx: {
          blue: "#4f7cff",
          "blue-deep": "#1e4ed8",
          navy: "#0d1b2a",
          orange: "#ff7e4d",
          "orange-deep": "#f64a1f",
          "orange-strong": "#c92c10",
          "blue-50": "#eef2ff",
          "blue-100": "#dce5ff",
          "blue-200": "#a8c1ff",
          "blue-300": "#7fa2ff",
          "blue-500": "#4f7cff",
          "blue-600": "#1e4ed8",
          "orange-50": "#fff1ec",
          "orange-100": "#ffd6cc",
          "orange-200": "#ffaa8f",
          "orange-500": "#ff7e4d",
          "orange-600": "#f64a1f",
          "orange-700": "#c92c10",
          "ink-50": "#f8fafc",
          "ink-100": "#eef2f6",
          "ink-200": "#e2e8f0",
          "ink-300": "#c3ceda",
          "ink-400": "#94a3b8",
          "ink-500": "#64748b",
          "ink-600": "#4c5a6b",
          "ink-700": "#364453",
          "ink-800": "#1e2a3a",
          "ink-900": "#0d1b2a",
          /**
           * Success, split by the job the colour is doing.
           *
           * One token could not do both. `success` was painted as small bold text — a status pill,
           * an upload phase — where it measured 4.39:1 on white and 3.94:1 on its own soft
           * background, under the 4.5:1 AA minimum for normal text; three separate surfaces had
           * already worked around it by reaching for a different colour rather than fixing it.
           * Darkening the one token to clear AA takes it to 2.87:1 against the dark theme's card,
           * under the 3:1 minimum for the icons and borders that are its other job. The two
           * requirements pull in opposite directions, so they are two tokens.
           *
           * `success` is the success *identity*: icons, borders, anything non-text, where WCAG asks
           * for 3:1 and this clears it on every surface in both themes (3.53:1 at its worst, the
           * dark card).
           *
           * `success-strong` is success *as text*, and only ever on a light surface — it is paired
           * with `success-soft` in a self-contained pill whose two halves are both fixed, so it
           * reads identically in either theme and is never painted on the dark card that would
           * undo it.
           */
          success: "#178a50",
          "success-strong": "#0f7a44",
          "success-soft": "#e8f6ef",
        },
      },
      fontFamily: {
        display: ["var(--font-display)", "system-ui", "sans-serif"],
        sans: ["var(--font-body)", "system-ui", "sans-serif"],
        mono: ["var(--font-mono)", "ui-monospace", "monospace"],
      },
      borderRadius: {
        sm: "6px",
        md: "10px",
        lg: "16px",
        xl: "24px",
        pill: "999px",
      },
      boxShadow: {
        sm: "0 1px 2px rgba(13,27,42,0.06)",
        md: "0 4px 12px rgba(13,27,42,0.08)",
        lg: "0 12px 32px rgba(13,27,42,0.12)",
        brand: "0 8px 24px rgba(79,124,255,0.28)",
        "brand-accent": "0 8px 24px rgba(255,126,77,0.30)",
      },
      backgroundImage: {
        "gradient-brand": "linear-gradient(135deg,#1e4ed8 0%,#4f7cff 100%)",
        "gradient-brand-soft": "linear-gradient(135deg,#eef2ff 0%,#f8fafc 100%)",
      },
      transitionTimingFunction: {
        "out-brand": "cubic-bezier(0.16,1,0.3,1)",
      },
      transitionDuration: {
        fast: "120ms",
        base: "200ms",
        slow: "320ms",
      },
      maxWidth: {
        container: "1200px",
      },
      keyframes: {
        "accordion-down": {
          from: { height: "0" },
          to: { height: "var(--radix-accordion-content-height)" },
        },
        "accordion-up": {
          from: { height: "var(--radix-accordion-content-height)" },
          to: { height: "0" },
        },
        "reveal-up": {
          from: { opacity: "0", transform: "translateY(18px)" },
          to: { opacity: "1", transform: "translateY(0)" },
        },
        "bird-float": {
          "0%,100%": { transform: "translateY(0) rotate(-4deg)" },
          "50%": { transform: "translateY(-16px) rotate(-1deg)" },
        },
      },
      animation: {
        "accordion-down": "accordion-down 200ms ease-out",
        "accordion-up": "accordion-up 200ms ease-out",
        "bird-float": "bird-float 6s cubic-bezier(0.16,1,0.3,1) infinite",
      },
    },
  },
  plugins: [require("tailwindcss-animate")],
};

export default config;
