# Gradex MVP Landing Page

> Status: Approved product/design baseline; current implementation drift is listed below
> Screen: [S01 — Landing](../../SCREENS.md)
> Last updated: 2026-07-23

The landing page introduces Gradex, lets visitors browse the catalog, and routes them to Student
registration or sign-in. It follows the repository design system; it does not define product scope,
commerce policy, or authorization independently.

Authoritative references:

- [Product requirements](../../PRD.md)
- [Business rules](../../BUSINESS_RULES.md)
- [Screen inventory](../../SCREENS.md)
- [Navigation rules](../../NAVIGATION_RULES.md)
- [Design system](../../design-system/README.md)

The adjacent [`index.html`](index.html) is an earlier visual prototype. It is useful for visual
comparison only. Its placeholder testimonials, installment question, and other stale copy are not
approved MVP requirements.

## Page Goals

1. Explain the Student value proposition without unsupported claims.
2. Let a visitor browse Published Courses and open Course Details.
3. Let a visitor create a Student account or sign in.
4. Demonstrate Arabic/English and responsive behavior from the first public screen.
5. Provide required legal and support links.

## Content Rules

- Arabic is the initial interface language; the saved language choice persists.
- All platform copy has natural Arabic and English versions with correct RTL/LTR behavior.
- Course titles/descriptions remain in their authored language; do not imply automatic translation.
- Use only real Published Course, price, Instructor, and catalog data.
- Do not show fabricated testimonials, ratings, enrollment counts, outcomes, credentials, or
  endorsements.
- An Instructor spotlight may appear only when the identity, claims, image, and quotation are real,
  current, and approved for public use. Otherwise omit the section.
- A testimonial section may appear only when real Student testimonials and explicit publication
  permission exist. Otherwise omit it entirely.
- Do not promise a Lab for every Course. Labs are optional protected lesson content.
- Do not advertise installments, certificates, subscriptions, native apps, built-in live video, or
  other non-MVP capabilities.
- Accessibility wording must use the boundary in the PRD: platform-owned UI/player controls target
  WCAG 2.2 AA; the site must not claim complete product conformance while captions/transcripts are
  outside MVP.

## Page Structure

### Header

- Brand mark links to the landing page.
- Public navigation links to Catalog and appropriate informational/legal pages.
- Actions: Sign in, Create Student account, and Browse Courses.
- Language control is visible and keyboard operable.
- On narrow screens, navigation moves into an accessible sheet/drawer without removing actions.
- An authenticated user sees a role-appropriate Dashboard action rather than a public registration
  prompt.

### Hero

- State a concise, supportable value proposition for university learning.
- Primary action: Browse Courses.
- Secondary action: Create Student account.
- Avoid a time-delayed, full-screen loader. Brand animation may be decorative only and must not block
  content, interaction, or performance; respect `prefers-reduced-motion`.
- Decorative imagery is hidden from assistive technology and must not imply unavailable features.

### Featured Courses

- Show a small data-driven selection of Published Courses, or an honest empty state.
- Each card links to Course Details and uses the catalog's actual title, Instructor, metadata, and
  Admin-controlled price.
- Price is formatted in KWD to three decimal places.
- Do not show ratings unless a real, approved rating capability and data source exist.
- Loading, empty, and error states must not fabricate catalog content.

### Why Gradex

- Explain approved value pillars such as structured learning, optional practical work, and
  Instructor support.
- Claims must be specific enough to verify and must not imply that optional features exist in every
  Course.
- Avoid unverified competitor comparisons and superlatives.

### Learning Experience

Explain the typical sequence without turning optional elements into promises:

1. Find a Course or Section.
2. Purchase through hosted checkout.
3. Learn through entitled Lessons and Resources.
4. Use available Labs or Course-scoped office hours where the Instructor has provided them.

### Optional Real-Identity Content

An Instructor spotlight and/or Student testimonials may sit here only when the content rules above
are satisfied. The layout must collapse cleanly when either section is absent. These sections are
not launch requirements.

### FAQ

Answer only questions supported by the approved MVP, including:

- what a Course or Section purchase grants;
- supported devices and responsive browser use;
- Arabic/English interface behavior versus Course content language;
- payment currency and hosted checkout;
- refund-policy location and how to request a refund;
- how to obtain support.

Use the shared accessible Accordion. Do not describe installments as available in MVP.

### Final Call to Action

- Primary action: Browse Courses.
- Secondary action: Create Student account.
- Repeat the approved value proposition without scarcity, fake urgency, or unsupported outcome
  claims.

### Footer

- Brand and concise description.
- Links to Catalog, About/Support where implemented, and role-appropriate authentication.
- Links to Terms, Privacy Notice, Refund Policy, and other launch-gated legal disclosures.
- KWD notice and accurate copyright/entity information.
- Include social links only for maintained official accounts.

## Responsive Behavior

- The public and Student experience is functionally complete on phones, tablets/iPads, laptops, and
  desktops.
- Layouts reflow based on content; horizontal scrolling is not required for core interaction.
- Calls to action remain visible and operable on narrow screens.
- Touch targets, focus visibility, reading order, and RTL behavior are verified at each supported
  layout.
- Hover effects have equivalent focus/touch presentation.

## Accessibility Acceptance

Within the platform-owned interface boundary:

- semantic `header`, `nav`, `main`, section headings, and `footer` landmarks;
- one page `h1` and logical heading order;
- a visible skip link and complete keyboard path;
- labelled language, menu, accordion, and authentication controls;
- visible focus, AA contrast, non-color-only status, and sufficient target sizes;
- no automatic motion that ignores reduced-motion preference;
- localized page title, landmarks, control labels, validation, and empty/error states.

## Data and State Requirements

- Featured Courses come from the Published Course catalog.
- Authentication state controls the header/dashboard action.
- Locale comes from the application locale provider and persists per approved behavior.
- Empty/error content is localized and actionable.
- Public preview links, when present, resolve only to the separate approved preview asset—not to a
  protected Lab or Resource.

## Current Implementation Drift

These are implementation follow-ups, not alternative requirements:

- `frontend/src/lib/i18n/config.ts` currently defaults to English; Arabic must be the initial
  default.
- `frontend/src/app/page.tsx` renders placeholder Testimonials. The section must be removed or
  hidden until real, consented testimonials exist.
- Current Course cards and Instructor content are seed/placeholders. They may support development
  but cannot be represented as real public catalog or identity data at launch.
- The earlier HTML prototype contains the same stale claims and is not a release artifact.

## Review Checklist

- Every visible claim maps to an approved MVP behavior or verified real data.
- Public actions resolve to implemented, authorized routes.
- Arabic is the initial default and all public copy works in RTL and LTR.
- Phone, tablet/iPad, laptop, and desktop layouts pass functional review.
- No protected content is exposed as a preview.
- No placeholder social proof or identity content can reach production.
- Legal links and launch disclosures are approved and available.
- Platform-owned accessibility acceptance checks pass without claiming broader conformance.
