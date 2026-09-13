/**
 * The courses surface rising into the Hero.
 *
 * The opaque surface stays still, so the scroll-linked facet movement can never expose a gap. Six
 * broad, uneven planes make the edge feel constructed rather than sawtoothed; the two quiet blue
 * overlays borrow the folded geometry of the GradeX bird mark.
 */
export function HeroCourseDivider() {
  return (
    <div
      aria-hidden="true"
      className="hero-course-divider pointer-events-none absolute inset-x-0 bottom-full h-9 translate-y-px overflow-hidden md:h-14"
    >
      <svg
        className="block size-full"
        viewBox="0 0 1440 56"
        preserveAspectRatio="none"
        focusable="false"
      >
        <defs>
          <path
            id="gx-divider-silhouette"
            d="M0 22L310 18L570 20L770 9L970 18L1110 15L1245 18L1350 14L1440 20V56H0Z"
          />
          <clipPath id="gx-divider-clip">
            <use href="#gx-divider-silhouette" />
          </clipPath>
        </defs>

        <use href="#gx-divider-silhouette" fill="hsl(var(--background))" />

        <g clipPath="url(#gx-divider-clip)">
          <path
            className="hero-divider-facet hero-divider-facet-forward"
            fill="hsl(var(--ring))"
            fillOpacity="0.14"
            d="M66 21L570 20L360 49L172 41Z"
          />
          <g
            className="hero-divider-facet hero-divider-facet-reverse"
            fill="hsl(var(--primary))"
            opacity="0.11"
          >
            <path d="M570 20L770 9L1110 15L890 46L670 39Z" />
            <path
              fill="hsl(var(--ring))"
              fillOpacity="0.72"
              d="M1110 15L1440 20L1245 42L890 46Z"
            />
          </g>
        </g>
      </svg>
    </div>
  );
}
