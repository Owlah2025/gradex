/**
 * The Subject catalogue's own copy, shared by the list, the card, the detail
 * page, and the demand control.
 *
 * It lives beside those screens for the same reason `catalogueCopy` does: every
 * entry is Subject vocabulary, and all four surfaces must render the same words
 * for the same thing. Two copies of "Request this course" is how the card and
 * the detail page start disagreeing.
 *
 * Vocabulary follows D-091 §12: a university Subject is `المادة` and a Gradex
 * Course is `الكورس`. The older `المقرر` in `catalogueCopy` is deliberately not
 * touched — §12 applies copy changes only on surfaces a tranche touches, and a
 * global rewrite is not authorized here.
 */
export const subjectCopy = {
  ar: {
    title: "المواد الدراسية",
    intro:
      "تصفّح مواد جامعتك. إذا كانت المادة متاحة كورس على جراديكس، تقدر تفتحها مباشرة. وإذا لسه مش متاحة، سجّل اهتمامك وإحنا نرتّب أولوياتنا على أساس طلبكم.",
    loading: "جارٍ تحميل المواد…",
    failed: "تعذر تحميل المواد. حاول مرة أخرى.",
    retry: "إعادة المحاولة",
    empty: "لا توجد مواد مطابقة.",
    emptyForInstitution: "لا توجد مواد مسجّلة لهذه الجامعة بعد.",
    searchLabel: "ابحث في المواد",
    searchPlaceholder: "ابحث برمز المادة أو اسمها",
    searchSubmit: "بحث",
    clearSearch: "مسح البحث",
    institutionLabel: "الجامعة",
    allInstitutions: "كل الجامعات",
    availabilityLabel: "التوفر",
    availabilityAll: "الكل",
    availabilityServed: "متاحة الآن",
    availabilityUnserved: "غير متاحة بعد",
    served: "متاحة الآن",
    unserved: "غير متاحة بعد",
    openCourse: "افتح الكورس",
    requestCourse: "اطلب هذا الكورس",
    requested: "تم تسجيل طلبك",
    withdraw: "إلغاء الطلب",
    withdrawing: "جارٍ الإلغاء…",
    requesting: "جارٍ التسجيل…",
    signInToRequest: "سجّل الدخول لتطلب هذا الكورس",
    signIn: "تسجيل الدخول",
    requestIntro:
      "هذا الكورس لسه مش متاح على جراديكس. سجّل اهتمامك وإحنا نرتّب أولوياتنا على أساس طلبات الطلبة.",
    requestedIntro:
      "سجّلنا طلبك. هنبلّغك أول ما يتاح الكورس. تسجيل الطلب لا يحجز مقعدًا ولا يضمن توفّر الكورس.",
    noteLabel: "ملاحظة اختيارية",
    notePlaceholder: "مثال: بدرس المادة دي الترم الجاي",
    noteTooLong: "الملاحظة أطول من ٥٠٠ حرف.",
    requestFailed: "تعذر تسجيل الطلب. حاول مرة أخرى.",
    withdrawFailed: "تعذر إلغاء الطلب. حاول مرة أخرى.",
    subjectGone: "هذه المادة لم تعد متاحة في الكتالوج.",
    unexpected: "حصل خطأ غير متوقع. لو تكرر، بلّغنا.",
    ineligible: "طلب الكورسات متاح لحسابات الطلبة فقط.",
    detailBack: "رجوع إلى المواد",
    coursesHeading: "الكورسات المتاحة لهذه المادة",
    code: "رمز المادة",
    university: "الجامعة",
    loadMore: "تحميل المزيد",
    loadingMore: "جارٍ التحميل…",
    showing: "عرض {shown} من {total}",
    resultCount: "مادة",
    resultCountPlural: "مادة",
  },
  en: {
    title: "Academic subjects",
    intro:
      "Browse the subjects your university teaches. Where a Gradex course already exists, you can open it. Where one does not, register your interest and we will prioritise what students actually ask for.",
    loading: "Loading subjects…",
    failed: "Could not load subjects. Try again.",
    retry: "Try again",
    empty: "No subjects match.",
    emptyForInstitution: "No subjects are recorded for this university yet.",
    searchLabel: "Search subjects",
    searchPlaceholder: "Search by subject code or name",
    searchSubmit: "Search",
    clearSearch: "Clear search",
    institutionLabel: "University",
    allInstitutions: "All universities",
    availabilityLabel: "Availability",
    availabilityAll: "All",
    availabilityServed: "Available now",
    availabilityUnserved: "Not available yet",
    served: "Available now",
    unserved: "Not available yet",
    openCourse: "Open the course",
    requestCourse: "Request this course",
    requested: "Requested",
    withdraw: "Withdraw request",
    withdrawing: "Withdrawing…",
    requesting: "Registering…",
    signInToRequest: "Sign in to request this course",
    signIn: "Sign in",
    requestIntro:
      "Gradex does not teach this subject yet. Register your interest and we will prioritise what students ask for.",
    requestedIntro:
      "Your request is recorded. We will let you know when the course is available. Requesting does not reserve a place and does not guarantee the course will be made.",
    noteLabel: "Optional note",
    notePlaceholder: "For example: I take this next term",
    noteTooLong: "The note is longer than 500 characters.",
    requestFailed: "Could not register your request. Try again.",
    withdrawFailed: "Could not withdraw your request. Try again.",
    subjectGone: "This subject is no longer in the catalogue.",
    unexpected: "Something unexpected went wrong. Tell us if it keeps happening.",
    ineligible: "Requesting courses is available to student accounts.",
    detailBack: "Back to subjects",
    coursesHeading: "Courses for this subject",
    code: "Subject code",
    university: "University",
    loadMore: "Load more",
    loadingMore: "Loading…",
    showing: "Showing {shown} of {total}",
    resultCount: "subject",
    resultCountPlural: "subjects",
  },
};

// Deliberately not `as const`. Const assertion narrows every value to its own
// English string literal, which makes the Arabic branch structurally
// incompatible with the type the components consume. `catalogueCopy` beside
// this file omits it for the same reason.
export type SubjectCopy = (typeof subjectCopy)["en"];
