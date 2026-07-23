# Gradex Design System

> Status: Active design guidance; implementation gaps noted below
> Last Updated: 2026-07-23

**Gradex — Graduate with excellence. تخرّج بتفوّق.**

This document describes the repository-backed visual and interaction system. Product scope and
behavior remain governed by [PRD.md](../PRD.md), [BUSINESS_RULES.md](../BUSINESS_RULES.md),
[SCREENS.md](../SCREENS.md), and [NAVIGATION_RULES.md](../NAVIGATION_RULES.md).

## Repository Sources

Design tokens:

- [`styles.css`](styles.css)
- [`tokens/colors.css`](tokens/colors.css)
- [`tokens/typography.css`](tokens/typography.css)
- [`tokens/fonts.css`](tokens/fonts.css)
- [`tokens/spacing.css`](tokens/spacing.css)
- [`tokens/effects.css`](tokens/effects.css)
- [`tokens/base.css`](tokens/base.css)

Current frontend implementation:

- [`frontend/src/app/globals.css`](../../frontend/src/app/globals.css)
- [`frontend/tailwind.config.ts`](../../frontend/tailwind.config.ts)
- [`frontend/src/components/`](../../frontend/src/components/)
- [`frontend/src/lib/i18n/`](../../frontend/src/lib/i18n/)

No uploaded PNG logo, external design export, missing `docs/design-system.md`, or missing UI-kit
directory is required by this document. The current repository mark is the vector
`BirdMark`/`Wordmark` implementation under `frontend/src/components/brand/`.

## Brand Essence

The origami bird represents a Student moving toward graduation. Gradex should feel supportive,
practical, confident, regional, and straightforward rather than institutional or hype-driven.

### Color Discipline

- Page/surface: slate/white (`#f8fafc` and semantic surface tokens).
- Primary: blue ramp (`#4f7cff`, AA-safe deep blue `#1e4ed8`).
- Accent: orange (`#ff7e4d`) for sparse high-value emphasis, not body text.
- Dark bands: navy (`#0d1b2a`).
- Semantic success/destructive/focus colors come from tokens, not arbitrary component hex values.

Components should use semantic Tailwind/shadcn tokens first and the `gx` brand ramp for deliberate
brand surfaces. Do not hardcode new color values in components.

### Typography

- **Alexandria:** display/headings/actions.
- **IBM Plex Sans Arabic:** body text in Arabic and Latin.
- **IBM Plex Mono:** Course codes, code, and technical/monetary islands where appropriate.

The frontend loads these through `next/font/google` in `frontend/src/app/layout.tsx`. The token CSS
font import is a standalone design reference, not the frontend runtime loading mechanism.

### Shape, Spacing, and Effects

- 4px spacing grid; common card padding 24px; wide marketing section spacing 64–96px.
- Radius scale: 6/10/16/24px and pill.
- Navy-tinted shadows; restrained lift on interactive cards.
- Calm ease-out motion using 120/200/320ms durations.
- Respect `prefers-reduced-motion`; motion cannot be required to understand or operate content.

## Language, Direction, and Content

- Product requirement: Arabic and English across all roles; Arabic is the initial default and the
  saved choice persists (BR-149).
- Switch `lang` and `dir` at the application shell. RTL affects navigation, breadcrumb order,
  chevrons, tables, forms, and motion direction.
- Preserve LTR islands for the wordmark, code, Course codes, and KWD prices where readability
  requires it.
- Course content remains in the Instructor's authored language and is not automatically translated.
- Use Western Arabic numerals (`0–9`) in both interfaces unless product research changes that rule.
- Display KWD with three decimal places: `38.000 KWD` / `38.000 د.ك`.
- Sentence case for UI actions/headings; all-caps only for short eyebrow labels.
- Voice is encouraging, direct, and specific. Arabic copy should be natural, not mechanical
  word-for-word English translation.
- Do not ship fabricated testimonials, ratings, enrollment claims, Instructor identities, or
  “recommended” labels without real data and permission.

## Responsive Layout

- Student flows provide complete functionality on phones, tablets/iPads, laptops, and desktops.
- Instructor/Admin flows remain responsive; complex authoring/operations may be optimized for
  tablet/laptop/desktop.
- Small screens use progressive disclosure (drawers/sheets/stacking), not feature removal.
- Common container maximum is 1200px. Grids collapse based on content needs, not device names alone.
- Video supports responsive sizing, landscape, keyboard controls, and browser fullscreen.
- Detailed shell behavior is authoritative in [NAVIGATION_RULES.md](../NAVIGATION_RULES.md).

## Accessibility

Platform-owned UI/player controls target WCAG 2.2 AA within the approved boundary:

- Semantic landmarks and heading order.
- Visible focus and complete keyboard path.
- Accessible authentication without cognitive/composition puzzles.
- Labelled fields, announced errors, and non-color-only states.
- AA contrast and appropriate target sizes.
- Reduced-motion handling.
- Decorative brand elements hidden from assistive technology; meaningful standalone mark labelled.

Captions/transcripts are outside MVP, so no design or marketing artifact may claim complete
learning-product WCAG conformance. Hosted checkout is evaluated but not represented as directly
controlled by Gradex.

## Current Component Inventory

Repository components currently include:

- **Brand:** `BirdMark`, `Logo`, `Scribble`, `Wordmark`.
- **Layout:** `Navbar`, `MobileNav`, `Footer`, `Container`, `Section`, `AuthActions`.
- **Common:** `LanguageToggle`, `ThemeToggle`, `Reveal`, `EmptyState`.
- **Course:** `CourseCard`.
- **UI:** `Accordion`, `Avatar`, `Badge`, `Button`, `Card`, `Sheet`, `Tag`, typography helpers.
- **Landing sections:** Hero, Featured Courses, Why Gradex, Learning Experience, Instructor
  Spotlight, Testimonials, FAQ, Final CTA.

This inventory describes current code, not the complete platform component plan. New components
must be derived from an approved Screen and reuse tokens/interaction patterns.

## Interaction Rules

- Hover must have a non-hover equivalent; touch/keyboard users cannot depend on hover content.
- Pressed, disabled, pending, success, and failure states must be explicit.
- Focus uses the semantic ring token and is never removed without an equal visible replacement.
- Dialog/sheet focus is trapped and restored; `Esc` closes when safe.
- Destructive/financial actions require clear scope and confirmation where appropriate.
- Price, refund, payout, publication, and suspension actions display immutable result/status rather
  than pretending a completed event is still editable form state.

## Known Implementation Drift

These are code follow-ups, not alternate requirements:

- `frontend/src/lib/i18n/config.ts` currently sets English as the default; product docs require
  Arabic as the initial default.
- The landing implementation contains a Testimonials section/data. It must remain hidden unless
  real, consented Student testimonials exist; fabricated placeholders cannot ship.
- Current frontend covers the marketing landing page, not the full Screen inventory in
  [SCREENS.md](../SCREENS.md).
- `frontend/src/data/courses.ts` placeholder cards carry a `level` field and a free-text `code`.
  The approved classification is one Major, one Subject (which owns the academic code), and one
  Study Year from the Admin-managed vocabulary; `level` is not an approved dimension.

## Design Review Checklist

- The screen/action exists in the approved MVP inventory.
- Role, ownership, price, and entitlement controls match Business Rules.
- Arabic/English and RTL/LTR are designed together.
- Phone, tablet/iPad, laptop, and desktop behavior is specified where relevant.
- Loading/empty/pending/error/denied states are present.
- Keyboard, focus, contrast, labels, and reduced motion are verified.
- Copy contains no unsupported legal, accessibility, testimonial, rating, or recommendation claim.
- No protected Resource/Lab is presented as a public preview.
