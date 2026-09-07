/**
 * Official university logos, once Gradex holds licensed copies of them.
 *
 * The academic option endpoints return a slug and the institution's name in both languages, and
 * nothing else — there is no logo on the contract and no asset for one in the repository. Rather
 * than invent a crest for a real university, the strip renders a typographic tile built from the
 * institution's own name until a real file is named here.
 *
 * To add one, drop the file under `public/university-logos/` and map it by the slug the catalogue
 * already uses:
 *
 *   "kuwait-university": { src: "/university-logos/kuwait-university.svg" },
 *
 * Nothing else changes: `UniversityStrip` already sizes the slot, centres the mark, and keeps the
 * institution's name as the accessible label. Only add marks Gradex is licensed to display.
 */

export type UniversityLogo = {
  /** Path under `public/`. SVG preferred — the slot is small and these are line marks. */
  src: string;
  /**
   * Optional per-logo scale, 0–1, for marks whose artwork carries its own padding and would
   * otherwise sit smaller than its neighbours in the same slot.
   */
  scale?: number;
};

export const universityLogos: Readonly<Record<string, UniversityLogo>> = {};
