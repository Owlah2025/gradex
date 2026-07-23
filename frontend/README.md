# Gradex Frontend

The current Next.js application implements the public landing-page slice and shared visual
components. It is not yet the complete MVP screen inventory.

Product behavior is governed by [`../docs/PRD.md`](../docs/PRD.md), and landing behavior by
[`../docs/design/landing-page/LANDING_SPEC.md`](../docs/design/landing-page/LANDING_SPEC.md).

## Verified Stack

- Next.js 14 App Router and React 18
- TypeScript with `strict: true`
- Tailwind CSS and Radix/shadcn-style primitives
- `next-themes` and Lucide icons
- Typed English/Arabic locale provider with RTL support

## Commands

From `frontend/`:

```bash
npm install
npm run dev
npm run build
npm run typecheck
npm run lint
```

These commands map directly to `package.json`. `npm run dev` uses Next.js's default local address,
normally `http://localhost:3000` when the port is available. The Google fonts loaded through
`next/font` may require network access during a production build.

## Structure

```text
src/
├── app/          # application shell, landing composition, metadata, and global CSS
├── components/   # brand, common, course, layout, landing-section, and UI components
├── config/       # site metadata/configuration
├── data/         # development seed content used by the landing slice
└── lib/          # types, helpers, and English/Arabic locale support
```

Use semantic theme tokens and existing components before adding a new visual pattern. The active
design reference is [`../docs/design-system/README.md`](../docs/design-system/README.md).

## Accessibility Boundary

The platform-owned interface targets WCAG 2.2 AA, including semantic structure, keyboard operation,
visible focus, reduced-motion handling, and AA contrast. Do not turn this into a claim that the
complete learning product currently conforms: captions/transcripts are outside the MVP and hosted
checkout is not fully controlled by Gradex.

## Known Product Drift

- `src/lib/i18n/config.ts` currently defaults to English; the approved requirement is Arabic as the
  initial default with a persistent language choice.
- `src/app/page.tsx` currently renders placeholder Testimonials. They must be removed or hidden
  unless real testimonials and publication consent exist.
- Course and Instructor data are development seed content and must not be represented as verified
  public catalog/identity data;
- linked auth, catalog, and dashboard routes are not yet implemented.

Treat these as implementation follow-ups. Do not change the approved documentation to match the
temporary landing implementation.
