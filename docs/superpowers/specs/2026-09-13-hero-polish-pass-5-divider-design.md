# Hero Polish Pass 5 — Divider Design

## Scope

Change only the visual boundary between the dark Hero and the lighter courses section. Preserve all
Hero copy, controls, selector behavior, section content, spacing, stacking, and scroll behavior.

## Design

Add a small decorative SVG transition to the top of the courses stack layer so it visually forms the
Hero's bottom edge while moving naturally with the existing stacked-section interaction.

The transition is primarily geometric:

- A fully opaque, shallow polygonal silhouette uses the semantic next-section background color.
- Broad angular planes form an asymmetric constructed edge without becoming a repetitive zigzag,
  sawtooth, or mountain-range pattern. Two additional shallow directional breaks enrich the right
  half without changing the silhouette's restrained height.
- Two crisp blue polygon overlays reference the folded planes of the GradeX bird mark. Extend and
  articulate the second plane farther right, and use restrained opacities near 0.14 and 0.11 so the
  facets remain subtle but readable.
- The transition is approximately 36px tall on mobile and 56px on larger screens.
- The abstract composition is direction-neutral and requires no LTR/RTL mirroring.

Remove the courses layer's current top border, rounded top edge, and top shadow so no straight seam
remains behind or beneath the SVG. The SVG may overlap upward into the Hero, but it must not change
normal-flow spacing or intercept input.

## Scroll Response

Reuse the landing stack's existing `--gx-stack-2` CSS view timeline to move the two translucent
facets 2–3px in opposing directions as the courses layer rises. Keep the opaque polygonal base
stationary so movement cannot expose a gap. Use no ambient loop and no JavaScript. Apply the
scroll-linked enhancement only under `prefers-reduced-motion: no-preference`; reduced-motion and
unsupported browsers receive the complete static composition.

## University Selector

Preserve `HeroAcademicPrompt` and its `UniversityStrip` in the existing lower-Hero position. The
56px desktop divider must remain at least 24px below the selector. `UniversityStrip` owns every
visual state in that same footprint:

- Ready: render the existing working institution chips unchanged.
- Loading: keep the localized question visible and show the shared localized `LoadingState`.
- Failed: keep the question visible and show the existing localized failure copy with a compact
  retry control.
- Ready but empty: keep the question visible and show the existing localized empty message.

Do not invent or hardcode institution data. Preserve the current early `null` return when a
signed-in student's profile already supplies resolved academic context.

## Implementation Shape

Use a focused, reusable decorative component colocated with the landing stack. It is `aria-hidden`,
pointer-event inert, lightweight, and contains no client state or JavaScript animation. Styling and
motion use existing Tailwind/CSS conventions and GradeX color tokens.

## Verification

- Confirm the Hero and courses content and behavior are unchanged.
- Confirm no border, shadow, pseudo-element, or SVG edge creates a straight horizontal seam.
- Run focused frontend static checks and relevant tests.
- Render and inspect desktop and mobile layouts in English and Arabic.
- Confirm the university selector is visible and unobstructed in EN desktop, AR desktop, EN mobile,
  and AR mobile for ready, loading, failed, and empty states where practical.
- Confirm the signed-in profile-resolved path remains absent as designed.
- Confirm the scroll-linked facets remain static under reduced motion and in browsers without CSS
  view-timeline support.
