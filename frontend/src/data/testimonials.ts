import type { Testimonial } from "@/lib/types";

/**
 * NOTE: pilot-cohort placeholders. Per the Gradex principle "honesty over hype",
 * do not ship fabricated reviews — replace with real, consented pilot quotes or
 * hide the section until they exist.
 */
export const testimonials: Testimonial[] = [
  {
    id: "fahd",
    quote: {
      en: "“The lab was the difference. I'd watched the same topic on another site and still couldn't do it — here I actually built it.”",
      ar: "«المعمل هو الفرق. كنت شاهدت نفس الموضوع على موقع ثاني وما قدرت أطبّقه — هنا فعلاً بنيته.»",
    },
    name: { en: "Fahd A.", ar: "فهد ع." },
    meta: { en: "Computer Science · Year 1", ar: "علوم حاسب · سنة أولى" },
    initial: "F",
  },
  {
    id: "ali",
    quote: {
      en: "“Asking a question at 1am and getting a real answer from the community got me through the week before finals.”",
      ar: "«سؤال الساعة ١ بالليل ورد حقيقي من المجتمع نجّاني الأسبوع اللي قبل الفاينل.»",
    },
    name: { en: "Ali R.", ar: "علي ر." },
    meta: { en: "Business · switching to CS", ar: "إدارة أعمال · متحوّل لعلوم حاسب" },
    initial: "A",
  },
  {
    id: "amjad",
    quote: {
      en: "“Studying in Arabic with the code in English is exactly how my brain works. Nothing else does this well.”",
      ar: "«المذاكرة بالعربي والكود بالإنجليزي هي بالضبط طريقة تفكيري. ما فيه شي ثاني يسوّيها بنفس الإتقان.»",
    },
    name: { en: "Amjad K.", ar: "أمجد ك." },
    meta: { en: "Prepping for university", ar: "يستعد للجامعة" },
    initial: "M",
  },
];
