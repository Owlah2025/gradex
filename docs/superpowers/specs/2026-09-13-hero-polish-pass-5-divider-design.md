# Hero Polish Pass 5 — Divider Design

## Scope

Change only the visual boundary between the dark Hero and the lighter courses section. Preserve all
Hero copy, controls, selector behavior, section content, spacing, stacking, and scroll behavior.

## Design

Add a small decorative SVG transition to the top of the courses stack layer so it visually forms the
Hero's bottom edge while moving naturally with the existing stacked-section interaction.

The transition is hybrid:

- A shallow, asymmetric curved silhouette uses the semantic next-section background color.
- Two faint translucent blue facets reference the GradeX origami geometry without reading as
  standalone triangles.
- SVG gradients fade the facets vertically into the next-section surface.
- The transition is approximately 36px tall on mobile and 56px on larger screens.
- The abstract composition is direction-neutral and requires no LTR/RTL mirroring.

Remove the courses layer's current top border, rounded top edge, and top shadow so no straight seam
remains behind or beneath the SVG. The SVG may overlap upward into the Hero, but it must not change
normal-flow spacing or intercept input.

## Motion

Animate only the translucent facet layer with a 2–3px transform drift over approximately eight
seconds using a smooth ease-in-out alternate loop. Keep the opaque curved silhouette stationary so
the section boundary never exposes gaps. Disable the drift under `prefers-reduced-motion: reduce`.

## Implementation Shape

Use a focused, reusable decorative component colocated with the landing stack. It is `aria-hidden`,
pointer-event inert, lightweight, and contains no client state or JavaScript animation. Styling and
motion use existing Tailwind/CSS conventions and GradeX color tokens.

## Verification

- Confirm the Hero and courses content and behavior are unchanged.
- Confirm no border, shadow, pseudo-element, or SVG edge creates a straight horizontal seam.
- Run focused frontend static checks and relevant tests.
- Render and inspect desktop and mobile layouts in English and Arabic.
- Confirm reduced-motion styling disables the ambient drift.

