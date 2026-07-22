# Gradex — MVP Landing Page (High-Fidelity Spec)

> Status: Ready for implementation
> Screen: `SCREENS.md → Landing` (Screen 1, Public)
> Design system: `_ds/gradex-design-system-f4d3887e…` (exported project, recovered from `Wireframe review.zip`)
> Mockup: [`index.html`](index.html) — self-contained, uses the DS tokens verbatim
> Target stack: Next.js (App Router) + Tailwind + shadcn/ui

This page **follows** the existing design system; it does not redesign it. Every color, type, radius, shadow, and motion value is a DS token. Where the brief asked for "components from the design system," this page uses only the DS families: Button, IconButton, Badge, Tag, Card, CourseCard, Avatar, Icon (Lucide), plus native `<details>` disclosure for the FAQ (a Card + summary, no new visual family).

---

## Design decisions grounded in the brand

- **Origami bird = a student taking flight.** It is the page's one signature: it *is* the loader, and it reappears ascending in the hero. Everything else stays quiet and disciplined (Chanel's "remove one accessory" — the bird earns the boldness, so no competing hero illustration).
- **Honesty over hype (Product Principle 6).** Pre-launch, the page shows **no fabricated stat-counters** (no "50,000 students"). Trust is built from concrete inclusions — labs in every course, bilingual, fair KWD pricing, instructor follow-up — and from an explicit, honest testimonial framing ("Voices from our pilot cohort. We'll only ever show reviews from students who actually took the course.").
- **Numbering only where it's a real sequence.** Only *Learning Experience* is numbered (01–04) because the steps genuinely build on each other. Featured courses and Why-Gradex pillars are parallel, so they carry no numbers — numbering there would be decoration.
- **Bilingual, Arabic-first.** A working `ع ⇄ EN` toggle flips `dir` and swaps every `.t` node. LTR islands are preserved for course codes, KWD prices, code snippets, "Java", and the wordmark, per the DS content rules.
- **Anti-Baims positioning, stated plainly.** One line under Why Gradex: "Fair price, not the cheapest — we compete on what you can build, never a race to the bottom."

### Color discipline (60 / 30 / 10)
- **60%** slate page `--surface-page #f8fafc` + white cards.
- **30%** primary blue `#4f7cff` — buttons, links, focus rings, icon chips.
- **10%** orange `#ff7e4d` — **exactly one pop per view**: the hero primary CTA, the final-CTA primary, "New" badges, the scribble underline, the bird's beak. Never a routine button, never body text.
- Navy `#0d1b2a` for the two dark bands (hero, final CTA) and the footer.
- One gradient element per view max (`--gradient-brand` on the hero glow / thumbnails).

### Type
- **Alexandria** (700–800) — all headings, buttons, eyebrows, prices-as-display.
- **IBM Plex Sans Arabic** — body (line-height 1.6–1.7; Arabic needs taller lines).
- **IBM Plex Mono** — course codes (`CS 101`), KWD prices (`38.000 KWD`), step numbers, code island.

---

## Loader (page-load moment)

**Purpose** — Brand-first entrance; stands in for the referenced *Animated Logo Hero* asset (see blocker note at the end).
**Layout** — Full-screen navy overlay, centered origami bird above the `Gradex` wordmark.
**Motion** — Bird rises + wing flaps, wordmark fades in, overlay lifts after ~1.7 s. `prefers-reduced-motion` → overlay is dismissed immediately, no animation.
**Implementation** — In Next.js, mount as a client component that removes itself on `window.load` (or route-ready), gated on `useReducedMotion()`.

---

## 1. Sticky Header

- **Purpose** — Constant access to nav + the two conversion paths (browse, register); orient guests and returning users.
- **Layout** — 64px, frosted `rgba(255,255,255,0.88)` + 12px blur, 1px bottom border. `[bird + wordmark] · [nav] · (auto) · [ع] [auth cluster]`.
- **Components** — BirdMark + Wordmark; nav links (Alexandria 600); `IconButton` language toggle; **guest cluster** = `Button ghost "Log in"` + `Button outline "Create account"` + `Button primary "Browse courses"`; **returning cluster** = notifications `IconButton` + `Button primary "Go to dashboard"` + `Avatar`. A preview-only chip (bottom-left) switches the two states; it is not part of the shipped page.
- **Visual hierarchy** — Blue solid "Browse courses" is the header's strongest element; "Create account" is the quieter outline; "Log in" is a text-weight ghost. Orange is deliberately **absent** here (reserved for hero).
- **Responsive** — ≤860px nav + desktop auth collapse into a right-side sheet opened by a hamburger `IconButton`; closes on scrim click / `Esc`.
- **Next/shadcn** — shadcn `NavigationMenu` + `Sheet` + `Button` variants (`primary→default`, `outline`, `ghost`). Returning state driven by session.

## 2. Hero

- **Purpose** — State the value proposition and route to Browse / Register in one screen.
- **Layout** — `min-height:88vh`, navy with a blue radial glow (top-right) and a faint orange glow (bottom-left). Two columns: copy (left) / visual (right).
- **Components** — Eyebrow "University courses · Kuwait"; `H1` "Graduate with excellence." with the **orange scribble underline** under *excellence.*; lead paragraph; `Button accent "Browse courses"` (the one orange pop) + `Button on-dark "Create account"`; a trust-chip list (Lucide check/languages/banknote icons). Visual = a floating `CourseCard` mock + a mono code island ("grade: passed ✓") + the ascending origami bird — no stock photography, per DS.
- **Visual hierarchy** — H1 (clamp 40→68px, weight 800) → lead → orange CTA → trust chips. The scribble draws attention to the single word "excellence" and nothing else.
- **Responsive** — ≤1024px stacks to one column; ≤560px the visual is hidden to keep the headline uncrowded and CTAs go full-width stacked.
- **Accessibility** — CTAs are ≥44px tall; orange-on-navy and white-on-navy both clear AA; the visual is `aria-hidden`.

## 3. Featured Courses

- **Purpose** — Prove the catalog is real and specific; drive Course Details.
- **Layout** — Left-aligned section head + a 3-up `CourseCard` grid.
- **Components** — `CourseCard`: gradient thumb + mono course-code badge + `Badge accent "New"`; `Tag` level + `Tag "Labs included"` (check icon); title; instructor row (`Avatar` + name); mono meta (lessons · hours); footer with mono `38.000 KWD` price + ghost "View". Section foot: `Button outline "Browse all courses"`.
- **Visual hierarchy** — Thumbnail/code → title → price. **No star ratings** (honest pre-launch) — a "New" badge stands in.
- **States** — Default / Loading (Card skeletons for the strip) / Empty ("Our first courses land soon — get notified"), matching `SCREENS.md`'s thin-launch note.
- **Responsive** — 3-col → 1-col at ≤860px.
- **Next/shadcn** — `CourseCard` component fed from the catalog API; shadcn `Skeleton` for loading; `Badge`, `Card`.

## 4. Why Gradex

- **Purpose** — Differentiate on the three USPs competitors drop: labs, community, follow-up.
- **Layout** — Centered head on a soft brand-tint band (`--gradient-brand-soft`), 3 parallel pillar Cards, one honest positioning line beneath.
- **Components** — `Card` + blue `iconchip` (Lucide `bird`/`users`/`shield-check`) + `H4` + body. Positioning note uses a banknote icon in accent.
- **Visual hierarchy** — Equal-weight pillars (parallel, unnumbered); the tint band separates this from the white sections around it.
- **Responsive** — 3-col → 1-col at ≤860px.

## 5. Learning Experience

- **Purpose** — Show that a Gradex course is a sequence that ends in *doing*, not watching.
- **Layout** — Section head + a 4-step row joined by a fading blue rail; each step = mono number, icon chip, H4, body.
- **Components** — `<ol>` of steps; `iconchip` (monitor-play, file-text, list-checks, then **accent** users chip on step 04 — the follow-up payoff mirrors the hero's orange).
- **Visual hierarchy** — The **only numbered section** (01–04, mono). Step 04 tints orange to land the "follow-up" promise.
- **Responsive** — 4-col → 2-col (≤1024px, rail hidden) → 1-col (≤560px).

## 6. Instructor Spotlight

- **Purpose** — Put a credible, regional human behind the courses; reinforce "instructors who stay."
- **Layout** — Two columns on brand-tint: instructor `Card` (left) / supporting copy + CTA (right).
- **Components** — Large `Avatar`, name + role, pull-quote, credential `Badge`s, a small stat strip (courses / lessons / AR·EN). Copy column: eyebrow, H2, two paragraphs, `Button primary "Meet the instructors"`.
- **Visual hierarchy** — The quote is the emotional anchor; stats are supporting, not vanity metrics.
- **Responsive** — 2-col → stacked at ≤1024px.
- **Note** — Instructor name/photo are **placeholders** (initial-avatar, no stock photo per DS). Replace with a real instructor + consent before launch.

## 7. Student Testimonials

- **Purpose** — Social proof, honestly framed for a pre-launch product.
- **Layout** — Head (with the honesty disclaimer) + 3 quote Cards.
- **Components** — `Card` `<figure>` → `blockquote` + `Avatar` + name + `CS · Year 1` line.
- **Visual hierarchy** — Quote first, attribution quiet.
- **Responsive** — 3-col → 1-col at ≤860px.
- **Note** — Quotes are **placeholder pilot voices**, flagged as such. Recommendation: do not ship fabricated testimonials — swap for real pilot quotes, or hide the section until they exist (Product Principle 6).

## 8. FAQ

- **Purpose** — Remove the specific objections that block a first purchase (what you get, instalments, language, after-purchase, devices).
- **Layout** — Centered head on brand-tint + a single narrow column (≤760px) of disclosure Cards.
- **Components** — Native `<details>/<summary>` styled as Cards with a rotating chevron — fully keyboard-accessible, no JS, no invented component.
- **Visual hierarchy** — Question (Alexandria 700) → answer (body). First item open by default.
- **Responsive** — Single column at all widths; comfortable tap targets.
- **Next/shadcn** — Map to shadcn `Accordion` (`type="single" collapsible`) with the same content.

## 9. Final CTA

- **Purpose** — Last, unambiguous conversion moment.
- **Layout** — Navy band with a blue radial glow; centered copy.
- **Components** — H2 "Ready to graduate with excellence?" + lead + `Button accent "Browse courses"` (this view's one orange pop) + `Button on-dark "Create free account"`.
- **Visual hierarchy** — Heading → orange CTA. Mirrors the hero to bookend the page.
- **Responsive** — CTAs wrap/stack on narrow screens.

## 10. Footer

- **Purpose** — Legal completeness (Kuwait Digital Commerce Law) + secondary navigation + brand close.
- **Layout** — Navy, 4 columns: brand + tagline + socials / Explore / Company / Legal, then a bottom bar.
- **Components** — BirdMark + Wordmark; `IconButton` socials (Discord, X, Instagram); link lists. Legal column: **Terms / Privacy / Refund policy** (required). Bottom: "© 2026 Gradex. Built in Kuwait." + "Prices in KWD."
- **Visual hierarchy** — Brand block widest; link columns equal and quiet.
- **Responsive** — 4-col → 2-col (≤860px) → 1-col (≤560px).

---

## Accessibility (WCAG AA)

- Semantic landmarks: `header[role=banner]`, `nav[aria-label]`, `main`, `section[aria-labelledby]`, `footer[role=contentinfo]` — verified in the a11y tree.
- Single `H1`; ordered `H2 → H3/H4` per section.
- Visible focus ring (`--ring-focus`, 3px blue) on every interactive element.
- `prefers-reduced-motion`: loader, scroll-reveal, scribble draw, and bird float all disable.
- Color pairs (white/navy, orange/navy, ink/slate) meet AA for their sizes; orange is never used for small body text on light.
- Decorative visuals `aria-hidden`; icons paired with text labels.
- Full keyboard path: nav → sheet (Esc to close) → FAQ disclosures → all CTAs.

## Implementation notes (Next.js + Tailwind + shadcn/ui)

1. Port `tokens/*.css` into `globals.css` `@layer base` and mirror them in `tailwind.config` (`colors.brand`, `fontFamily.display/body/mono`, `borderRadius`, `boxShadow`). Do not hardcode hex in components.
2. Build DS components as the shadcn equivalents noted per section; keep the `.gx-*` utility semantics (`gx-container`, `gx-eyebrow`, `gx-reveal`).
3. `Reveal` = a client wrapper using IntersectionObserver at threshold 0.15 (already the DS pattern).
4. Featured courses, instructor, and testimonials should be data-driven; wire loading/empty states from `SCREENS.md`.
5. RTL: drive `dir` from locale (next-intl or similar); keep the LTR islands (course code, price, code, wordmark) with `dir="ltr"`.

## Blocked / follow-up

- **`claude_design` MCP import** (`/design/p/f4d3887e…`) and the **Animated Logo Hero** (`/design/p/83ae09bc…`) both require `/design-login` OAuth, which needs an interactive terminal — unavailable in this session. The design system was instead recovered from the `Wireframe review.zip` already in the repo (same project id). To pull the canonical animated logo, authorize in an interactive `claude` session (or claude.ai connector settings) and re-import; then swap it into the loader in place of the stand-in bird.
- Replace placeholder instructor + testimonials with real, consented content before launch.
