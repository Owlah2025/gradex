# Gradex — Frontend

Production Next.js app for Gradex. This slice ships the **Landing** page
(`SCREENS.md` → Screen 1) plus the shared design-language components every
later screen composes from.

## Stack

- **Next.js 14** (App Router) · **TypeScript** (strict)
- **Tailwind CSS** + **shadcn/ui** primitives (Radix under the hood)
- **next-themes** (dark mode, `class` strategy)
- **lucide-react** icons
- Bilingual **English ⇄ Arabic (RTL)** via a typed locale provider

## Getting started

```bash
npm install
npm run dev        # http://localhost:3000
npm run build      # production build
npm run typecheck  # tsc --noEmit
npm run lint
```

> Webfonts (Alexandria, IBM Plex Sans Arabic, IBM Plex Mono) load from Google
> Fonts via `next/font` at build time — the build machine needs network access
> to `fonts.googleapis.com`.

## Design system → code

The design system lives in [`../docs/design-system`](../docs/design-system)
(recovered from the exported Claude Design project `f4d3887e…`). Its tokens are
wired in exactly once:

- **Colour / radius / shadow / motion tokens** → `tailwind.config.ts` +
  `src/app/globals.css` (semantic shadcn HSL tokens for light **and** dark, plus
  the raw `gx.*` brand ramp). **Never hardcode a hex in a component.**
- **60 / 30 / 10 rule** — slate surfaces, blue primary, orange accent used for
  exactly one "pop" per view (hero + final CTA). Orange never for routine
  buttons or body text.
- **Type** — `font-display` (Alexandria) for headings/buttons, `font-sans`
  (IBM Plex Sans Arabic) for body, `font-mono` for course codes + KWD prices.

### WCAG AA deviations from the raw tokens (intentional)

- `--primary` is **blue-600** (not blue-500) so white-on-primary clears 4.5:1.
  blue-500 stays the focus ring / decorative accent.
- The orange accent button uses **navy text** (white-on-orange fails AA).

## Architecture

`page.tsx` is pure composition — it renders section components and nothing else.
Markup and logic live in reusable components:

```
src/
├─ app/
│  ├─ layout.tsx            # fonts, Providers, metadata, skip link, <html lang/dir>
│  ├─ page.tsx              # Landing — composes sections only
│  ├─ globals.css           # tokens (light/dark) + base layer
│  ├─ opengraph-image.tsx   # dynamic OG image (next/og)
│  ├─ icon.svg              # favicon (bird mark)
│  ├─ sitemap.ts · robots.ts
├─ components/
│  ├─ ui/                   # shared design language (shadcn-style, reused everywhere)
│  │  ├─ button · card · badge · tag · avatar · accordion · sheet · typography
│  ├─ layout/               # navbar · mobile-nav · footer · container · section · auth-actions
│  ├─ brand/                # logo · wordmark · bird-mark · scribble
│  ├─ common/               # reveal · empty-state · theme-toggle · language-toggle
│  ├─ course/               # course-card  (reused by Catalog + Course Details later)
│  ├─ sections/             # hero · featured-courses · why-gradex · learning-experience
│  │                        #  instructor-spotlight · testimonials · faq · final-cta
│  └─ providers.tsx         # ThemeProvider + LocaleProvider
├─ lib/
│  ├─ utils.ts              # cn()
│  ├─ types.ts              # Course / FaqItem / Testimonial / Localized
│  └─ i18n/                 # config · locale-provider · dictionaries/{en,ar}
├─ data/                    # courses · faq · testimonials (placeholder, see notes)
└─ config/site.ts           # metadata + brand config
```

**Building the next screens (Catalog, Course Details, …):** import from
`components/ui`, `components/layout`, and `components/course` — do not invent new
UI. Add screen-specific composition under `components/sections` (or a new
folder) and a thin route in `app/`.

## Accessibility

Semantic landmarks (`header`/`nav`/`main`/`section[aria-labelledby]`/`footer`),
single `h1`, visible focus rings, skip link, keyboard-operable sheet + accordion,
`prefers-reduced-motion` respected, AA contrast (see deviations above).

## Notes / follow-up

- Auth routes (`/login`, `/register`, `/courses`, `/dashboard`) are linked but
  not built yet — they are later screens in `DESIGN_ORDER.MD`.
- `data/courses.ts`, `data/testimonials.ts` are **placeholders**. Per Gradex's
  "honesty over hype" principle, replace testimonials with real consented pilot
  quotes (or hide the section) before launch, and wire courses to the catalog API.
- Locale switching is client-side. To scale to full localized routing/SEO across
  34 screens, migrate to `next-intl` with an `[locale]` segment — the dictionary
  shape here already matches that model.
