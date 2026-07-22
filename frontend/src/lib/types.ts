import type { Locale } from "./i18n/config";

/** A string localized for each supported locale. */
export type Localized = Record<Locale, string>;

export type CourseLevel = "beginner" | "intermediate" | "advanced";

export interface Course {
  slug: string;
  /** Course code — always rendered LTR (e.g. "CS 101"). */
  code: string;
  title: Localized;
  /** Instructor display name — Localized so Arabic UI shows an Arabic name. */
  instructor: Localized;
  instructorInitial: string;
  level: CourseLevel;
  lessons: number;
  /** Runtime, LTR (e.g. "18h"). */
  duration: string;
  /** Price in KWD, 3 decimals, rendered LTR (e.g. "38.000 KWD"). */
  price: string;
  labsIncluded: boolean;
  isNew: boolean;
  /** Tailwind gradient class for the card thumbnail. */
  thumb: string;
}

export interface FaqItem {
  id: string;
  question: Localized;
  answer: Localized;
}

export interface Testimonial {
  id: string;
  quote: Localized;
  name: Localized;
  meta: Localized;
  initial: string;
}
