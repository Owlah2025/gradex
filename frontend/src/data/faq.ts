import type { FaqItem } from "@/lib/types";

export const faqItems: FaqItem[] = [
  {
    id: "what-you-get",
    question: {
      en: "What do I actually get when I buy a course?",
      ar: "ماذا أحصل فعلاً عند شراء المقرر؟",
    },
    answer: {
      en: "A Course purchase covers all of its Sections; a Section purchase covers that Section only. Access lasts 150 days from purchase and includes the protected videos plus any Resources and Lab Materials available in the purchased scope.",
      ar: "شراء المقرر يمنحك الوصول إلى جميع أقسامه، بينما شراء قسم يمنحك الوصول إلى ذلك القسم فقط. يستمر الوصول 150 يوماً من تاريخ الشراء ويشمل الفيديوهات المحمية وأي مصادر ومواد معملية متاحة ضمن ما اشتريته.",
    },
  },
  {
    id: "payment",
    question: {
      en: "How do I pay?",
      ar: "كيف أدفع؟",
    },
    answer: {
      en: "Course and Section prices are shown in KWD. Payment is completed on Tap's hosted checkout, so Gradex does not collect or store your card details.",
      ar: "تظهر أسعار المقررات والأقسام بالدينار الكويتي. يتم الدفع عبر صفحة الدفع المستضافة من Tap، لذلك لا تجمع Gradex بيانات بطاقتك ولا تخزنها.",
    },
  },
  {
    id: "language",
    question: {
      en: "Are the courses in Arabic or English?",
      ar: "المقررات بالعربي أو الإنجليزي؟",
    },
    answer: {
      en: "The Gradex interface is available in Arabic and English. Each Course keeps the language chosen by its Instructor, which is shown on the Course details; Gradex does not automatically translate Course content.",
      ar: "واجهة Gradex متاحة بالعربية والإنجليزية. يحتفظ كل مقرر باللغة التي اختارها المدرّس وتظهر هذه اللغة في تفاصيل المقرر؛ ولا تترجم Gradex محتوى المقرر تلقائياً.",
    },
  },
  {
    id: "after-course",
    question: {
      en: "What happens after I finish a course?",
      ar: "ماذا يحدث بعد إنهاء المقرر؟",
    },
    answer: {
      en: "Your Course or Section access remains active through the end of day 150 in Kuwait time. Your learning progress remains recorded after access expires, and you can purchase access again through the normal checkout.",
      ar: "يبقى وصولك إلى المقرر أو القسم فعالاً حتى نهاية اليوم 150 بتوقيت الكويت. يظل تقدمك الدراسي محفوظاً بعد انتهاء الوصول، ويمكنك شراء الوصول مجدداً عبر الدفع المعتاد.",
    },
  },
  {
    id: "devices",
    question: {
      en: "Can I watch on my phone?",
      ar: "أقدر أشاهد على الجوال؟",
    },
    answer: {
      en: "Yes. Video adapts to your connection and resumes from your last position across phone, tablet, and laptop.",
      ar: "نعم. الفيديو يتكيّف مع اتصالك ويكمل من آخر موضع توقفت عنده على الجوال والتابلت واللابتوب.",
    },
  },
];
