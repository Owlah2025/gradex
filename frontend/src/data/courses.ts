import type { Course } from "@/lib/types";

/**
 * Launch catalog placeholder data.
 * TODO(catalog): replace with the catalog API (see SCREENS.md → Catalog).
 * Prices in KWD to 3 decimals per the design system content rules.
 */
export const featuredCourses: Course[] = [
  {
    slug: "intro-to-programming",
    code: "CS 101",
    title: { en: "Intro to programming", ar: "مقدمة في البرمجة" },
    instructor: { en: "Dr. Sara Al-Mutairi", ar: "د. سارة المطيري" },
    instructorInitial: "S",
    level: "beginner",
    lessons: 42,
    duration: "18h",
    price: "38.000 KWD",
    labsIncluded: true,
    isNew: true,
    thumb: "bg-[linear-gradient(135deg,#1e4ed8,#4f7cff)]",
  },
  {
    slug: "data-structures-java",
    code: "CS 201",
    title: { en: "Data structures in Java", ar: "هياكل البيانات بلغة Java" },
    instructor: { en: "Yousef Al-Enezi", ar: "يوسف العنزي" },
    instructorInitial: "Y",
    level: "intermediate",
    lessons: 55,
    duration: "24h",
    price: "45.000 KWD",
    labsIncluded: true,
    isNew: false,
    thumb: "bg-[linear-gradient(135deg,#0d1b2a,#364453)]",
  },
  {
    slug: "calculus-i",
    code: "MATH 110",
    title: { en: "Calculus I, made survivable", ar: "تفاضل وتكامل ١ ببساطة" },
    instructor: { en: "Dr. Noura Al-Sabah", ar: "د. نورة الصباح" },
    instructorInitial: "N",
    level: "beginner",
    lessons: 30,
    duration: "14h",
    price: "32.000 KWD",
    labsIncluded: true,
    isNew: true,
    thumb: "bg-[linear-gradient(135deg,#4f7cff,#7fa2ff)]",
  },
];
