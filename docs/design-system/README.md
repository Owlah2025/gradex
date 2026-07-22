# Gradex Design System

**Gradex — Graduate with excellence. تخرّج بتفوّق.**

Gradex is a university-courses platform for GCC students (launching Kuwait-first) offering high-quality lecture videos, slides, and study notes with follow-up from top instructors. Computer-science focus. Fully bilingual: Arabic (RTL, primary voice) and English (LTR); course content and code stay in English.

## Sources

- GitHub: https://github.com/Owlah2025/gradex — Next.js frontend (`frontend/src/app/globals.css`, `frontend/src/components/ui/DesignSystem.tsx`), full spec docs (`docs/design-system.md`, `docs/GradeX_Full_Frontend_Specification.md`), and a prior landing redesign (`docs/Gradex Landing Page Redesign/`). Explore these to go deeper when designing for Gradex.
- `uploads/color-palette.png` — the current brand palette (60/30/10: light slate / primary blue / orange accent). **This palette supersedes the legacy teal/sand palette documented in the repo.** Structure, components, type, and voice still follow the repo.
- `uploads/Untitled design.png` — **the official logo** (full lockup on white). Processed into `assets/logo-full.png` (transparent lockup: navy "Grade" + blue-to-red gradient "e"/"x" swash + bird) and `assets/logo-bird.png` (bird mark alone). These are the canonical marks — use them wherever a logo goes; `assets/logo.svg` is only a stylized vector approximation for tiny inline uses.

## Brand essence

The origami bird taking flight = a student rising toward graduation. Palette: **cool slate paper (60%), confident primary blue (30%), warm orange accent (10%)** — the orange is reserved for the one CTA per view that must pop, highlights, and the bird's beak. Elegant, simple, pleasant; regional and studious.

## Content fundamentals

- **Voice:** encouraging mentor, not corporate. Speaks to "you" (أنت). Short sentences. Never slang, never stiff. e.g. "Notes that get to the point" / "ملخصات تختصر الطريق"; "Check yourself after every lesson" / "اختبر نفسك بعد كل درس".
- **Casing:** sentence case everywhere — headings and buttons ("Browse courses", not "Browse Courses"). ALL CAPS only for tiny eyebrow labels.
- **Bilingual:** Arabic first for Kuwait; the UI mirrors cleanly RTL⇄LTR. Course names, course codes, and code samples stay English/LTR even in Arabic UI (`dir="ltr"` islands). Wordmark always renders LTR.
- **Numbers:** Western Arabic numerals (0-9) in both languages. Prices in KWD with 3 decimals ("38.000 KWD" / "38.000 د.ك").
- **Emoji:** never in product UI (sparingly in notifications only).
- Example hero pairs: "Graduate with excellence." / "تخرّج بتفوّق." — "Your courses. Your pace. Your language." / "مقرراتك، بوتيرتك، وبلغتك."

## Visual foundations

- **Color:** 60% `--surface-page` #f8fafc backgrounds + white cards; 30% primary blue #4f7cff (buttons, links, focus, icons); 10% orange #ff7e4d (hero CTA, badges, highlights). Dark bands and footer use navy `--ink-900` #0d1b2a. Never yellow/gold. Orange never for routine buttons or body text.
- **Gradient:** `--gradient-brand` (deep blue → blue, 135deg) for hero moments — one gradient element per view, max.
- **Type:** Alexandria (geometric, Kufi-influenced, Arabic+Latin) for display/headings/buttons — always bold 700-800, never light. IBM Plex Sans Arabic for body (line-height 1.6-1.7; Arabic needs taller lines). IBM Plex Mono for code and course codes. Loaded from Google Fonts CDN (`tokens/fonts.css`).
- **Spacing:** 4px grid; cards pad 24px; marketing sections breathe 64-96px block; container max 1200px.
- **Corners:** soft everywhere, never sharp — 6/10/16/24px + pill. Cards 16px, inputs 10px, large panels 24px.
- **Shadows:** navy-tinted, never gray-black (`rgba(13,27,42,…)`); primary CTAs carry a blue glow (`--shadow-brand`).
- **Cards:** white, 1px `--border-default`, radius 16px, `--shadow-sm` resting → `--shadow-md` + `translateY(-2px)` on hover.
- **Motion:** calm ease-out (`cubic-bezier(0.16,1,0.3,1)`), 120/200/320ms. Never bouncy. Two signature patterns (see `guidelines/motion.html` and the UI kit):
  - **Scroll reveal** (`.gx-reveal` → `.gx-in` via IntersectionObserver at threshold 0.15): fade + 18px rise.
  - **Cover-scroll stack** (`.gx-stack`): each full-height section is `position:sticky; top:0`, so the section you're in holds while the next one slides up and covers it, with `--shadow-stack` under the incoming edge. Respect `prefers-reduced-motion`.
- **States:** hover buttons darken one step; hover cards lift; links underline; press darkens + `scale(0.98)`; focus 3px soft blue ring; disabled opacity 0.5.
- **Imagery:** no stock photos — the origami bird, flat color blocks, UI mocks, and code snippets carry the visuals. Hand-drawn scribble underline / circle sketch (orange gradient strokes) accent one word in a heading.
- **Transparency/blur:** frosted sticky nav only (`rgba(255,255,255,0.88)` + 12px blur).
- **Layout:** sticky 64px nav; hero min-height 88vh, dark navy with overlay; alternating two-column feature rows; 2/3-column card grids collapsing at ~860px.

## Iconography

- **Lucide** (https://lucide.dev), inlined SVGs — stroke `currentColor`, 2px, round caps/joins. Sizes 16/20/22/24. The repo's set is reproduced in `components/brand/Icon.jsx` (play, shield, fileText, listChecks, languages, banknote, smartphone, users, check, bell, arrowRight, gradCap, monitorPlay). Extend from Lucide only, matching stroke weight.
- No emoji as icons, no unicode-glyph icons, no hand-rolled SVG art.
- Brand marks: `assets/logo-full.png` (official lockup), `assets/logo-bird.png` (bird mark), `assets/logo.svg` (stylized vector approx), `assets/logo-teal-legacy.svg` (previous teal). `BirdMark` + `Wordmark` components are vector stand-ins for tiny sizes. Wordmark = "Grade" in ink + "x" in orange accent, Alexandria extrabold, always LTR.

## Index

- `styles.css` → `tokens/` (fonts, colors, typography, spacing, effects, base + `.gx-*` utilities)
- `assets/` — logo-full.png (official lockup), logo-bird.png (mark), logo.svg, logo-teal-legacy.svg
- `guidelines/` — foundation specimen cards (colors, type, spacing, radii, shadows, motion, brand)
- `components/brand/` — BirdMark, Wordmark, Icon, Scribble, CircleSketch, Reveal
- `components/buttons/` — Button, IconButton
- `components/display/` — Badge, Tag, Card, CourseCard, ProgressBar, Tabs, Avatar
- `components/forms/` — Input, Select, Checkbox, Radio, Switch
- `components/feedback/` — Dialog, Toast, Tooltip
- `ui_kits/website/` — the Gradex web platform: landing (EN/AR toggle, cover-scroll stack) + login
- `SKILL.md` — agent skill entry point

Component inventory follows `docs/design-system.md` §12 in the repo verbatim (no invented families). **Intentional additions:** `Icon` (wrapper for the inlined Lucide set), `Reveal` (scroll-reveal wrapper the repo defines inside pages).

## Caveats

- Colors were remapped from the uploaded palette; teal values in repo docs are legacy.
- `uploads/logo.svg` is broken (empty embedded image) — please re-upload the real logo file if the recolored bird isn't exact.
- Fonts load from Google Fonts CDN, not self-hosted binaries.
