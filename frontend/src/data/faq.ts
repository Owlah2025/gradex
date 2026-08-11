import type { FaqItem } from "@/lib/types";

export const faqItems: FaqItem[] = [
  {
    id: "course-access",
    question: {
      en: "How do I get access to a course?",
      ar: "كيف أحصل على الوصول إلى مقرر؟",
    },
    answer: {
      en: "Course access is granted through a Course Access Invitation for your email. After you accept it, an Admin reviews and approves the invitation. Only that approval creates access to the invited course.",
      ar: "يُمنح الوصول إلى المقرر عبر دعوة وصول للمقرر مرتبطة ببريدك. بعد قبولها، يراجع المسؤول الدعوة ويوافق عليها. هذه الموافقة وحدها تنشئ الوصول إلى المقرر المدعو إليه.",
    },
  },
  {
    id: "payment",
    question: {
      en: "How do I pay?",
      ar: "كيف أدفع؟",
    },
    answer: {
      en: "Any payment arrangement is handled outside Gradex. Once it is confirmed through the applicable external process, an Admin can create the Course Access Invitation for the intended student.",
      ar: "يُرتّب أي دفع خارج Gradex. بعد تأكيده عبر الإجراء الخارجي المناسب، يمكن للمسؤول إنشاء دعوة وصول المقرر للطالب المقصود.",
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
      en: "Your access period is shown with your authorised course access. If it ends, contact the Gradex team through the applicable access process; Gradex does not take payments or renew access automatically on the platform.",
      ar: "تظهر مدة وصولك مع وصول المقرر المصرّح به. إذا انتهت، تواصل مع فريق Gradex عبر إجراء الوصول المناسب؛ لا يتلقى Gradex المدفوعات أو يجدّد الوصول تلقائياً داخل المنصة.",
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
