export const en = {
  meta: {
    switchToAria: "Switch language to Arabic",
    themeToggleAria: "Toggle dark mode",
    openMenu: "Open menu",
    closeMenu: "Close menu",
    skipToContent: "Skip to content",
    logoHomeAria: "Gradex home",
  },
  nav: {
    courses: "Courses",
    why: "Why Gradex",
    instructors: "Instructors",
    faq: "FAQ",
    login: "Log in",
    register: "Create account",
    browse: "Browse courses",
    dashboard: "Go to dashboard",
    notifications: "Notifications",
  },
  auth: {
    shell: {
      eyebrow: "Student access",
      sideTitle: "Start with one clear next step.",
      sideBody:
        "Create your Student account, confirm your email, then return to sign in when access opens.",
      privacy: "Your password and verification link stay in this browser flow only.",
      language: "Language",
      steps: ["Create account", "Confirm email", "Sign in"],
    },
    register: {
      title: "Create your Student account",
      intro: "Use an email you can open now. We’ll ask you to confirm it next.",
      displayName: "Display name",
      displayHint: "2–50 Arabic or Latin characters. This can be changed later.",
      email: "Email address",
      password: "Password",
      passwordHint: "15–128 characters. Spaces are welcome; there are no symbol rules.",
      policiesLoading: "Loading the current terms…",
      policiesUnavailable: "The current terms could not be loaded.",
      acceptPrefix: "I have read and accept",
      create: "Create account",
      creating: "Creating account…",
      invalidName: "Enter a name using 2–50 Arabic or Latin characters.",
      invalidEmail: "Enter a complete email address.",
      invalidPassword: "Use between 15 and 128 characters.",
      acceptPolicies: "Accept each current policy to continue.",
      failed: "The account request could not be accepted.",
    },
    verify: {
      title: "Check your email",
      intro:
        "Enter your email to request a fresh verification link. The response is the same whether or not an account is found.",
      email: "Email address",
      send: "Request verification link",
      sending: "Requesting…",
      acceptedTitle: "Request accepted",
      acceptedBody:
        "If this address is eligible, a new link will be prepared. Check your inbox and spam folder.",
      retry: "Try again",
      limited: "Too many attempts. Wait a little before trying again.",
      unavailable: "Verification requests are temporarily unavailable. Try again shortly.",
    },
    result: {
      title: "Confirming your email",
      intro: "Keep this page open while Gradex checks the one-time link.",
      checking: "Checking the verification link…",
      successTitle: "Email confirmed",
      successBody:
        "Your Student account is active. Sign in becomes available in the next launch step.",
      invalidTitle: "This link is unavailable",
      invalidBody:
        "The link may be expired, already used, or replaced. Request a fresh link to continue.",
      requestNew: "Request a new link",
      login: "Go to login",
    },
    login: {
      title: "Sign in to Gradex",
      intro: "Use the email and password you confirmed for your account.",
      email: "Email address",
      password: "Password",
      signIn: "Sign in",
      signingIn: "Signing in…",
      forgotPassword: "Forgot your password?",
      noAccount: "Need an account?",
      createAccount: "Create one",
      invalidEmail: "Enter a complete email address.",
      invalidPassword: "Enter your password.",
      // Deliberately one message for unknown email, wrong password, unverified,
      // and inactive. The server hides which one occurred and the interface
      // must not narrow it back down.
      failed: "The email or password is incorrect.",
      limited: "Too many attempts. Wait a little before trying again.",
      unavailable: "Sign-in is temporarily unavailable. Try again shortly.",
    },
    recover: {
      title: "Reset your password",
      intro:
        "Enter your email and we will prepare a reset link. The response is the same whether or not an account is found.",
      email: "Email address",
      send: "Send reset link",
      sending: "Sending…",
      acceptedTitle: "Check your email",
      acceptedBody:
        "If this address belongs to an active account, a reset link is on its way. The link can be used once and expires.",
      invalidEmail: "Enter a complete email address.",
      limited: "Too many attempts. Wait a little before trying again.",
      unavailable: "Password reset is temporarily unavailable. Try again shortly.",
      failed: "The request could not be completed. Try again shortly.",
      backToSignIn: "Back to sign in",
    },
    staff: {
      createTitle: "Invite Staff Member",
      createIntro: "Send an invitation to a new instructor or administrator.",
      email: "Email address",
      role: "Role",
      roleInstructor: "Instructor",
      roleAdmin: "Administrator",
      sendInvite: "Send Invitation",
      sendingInvite: "Sending invitation…",
      listTitle: "Pending Staff Invitations",
      noPending: "No pending staff invitations.",
      revoke: "Revoke",
      revoking: "Revoking…",
      suspendTitle: "Suspend Account",
      suspendReason: "Reason for suspension",
      suspendAction: "Suspend Account",
      suspending: "Suspending account…",
      reinstateTitle: "Reinstate Account",
      reinstateReason: "Reason for reinstatement (optional)",
      reinstateAction: "Reinstate Account",
      reinstating: "Reinstating account…",
      onboardTitle: "Complete Staff Onboarding",
      onboardIntro: "Set your display name and password to complete your account setup.",
      displayName: "Display name",
      password: "Password",
      completeOnboarding: "Complete Onboarding",
      completingOnboarding: "Completing onboarding…",
    },
    resetPassword: {
      title: "Choose a new password",
      intro: "This link works once. After resetting, sign in with your new password.",
      checking: "Opening your reset link…",
      password: "New password",
      confirm: "Confirm new password",
      submit: "Reset password",
      submitting: "Resetting…",
      mismatch: "Both passwords must match.",
      weak: "Choose a longer password. Use at least 15 characters.",
      // One message for expired, already used, superseded, and unknown links.
      // The server refuses all four identically and the interface must not
      // narrow it back down.
      invalidLink: "This reset link is no longer valid. Request a new one.",
      limited: "Too many attempts. Wait a little before trying again.",
      unavailable: "Password reset is temporarily unavailable. Try again shortly.",
      failed: "The password could not be reset. Try again shortly.",
      successTitle: "Password reset",
      successBody:
        "Every signed-in device was signed out. Sign in with your new password to continue.",
      goToSignIn: "Go to sign in",
      missingToken: "This page needs a reset link. Request one to continue.",
      requestNew: "Request a new link",
    },
    session: {
      expiredTitle: "Your session ended",
      expiredBody: "Sign in again to pick up where you left off.",
      replacedTitle: "This session was replaced",
      replacedBody:
        "Your account was refreshed in another tab or window. Sign in again to continue.",
      reuseTitle: "This session was closed for your safety",
      reuseBody:
        "An out-of-date session credential was presented, so every session for your account was ended. Sign in again to continue.",
      signedOutTitle: "You are signed out",
      signedOutBody: "Your session was ended on this device.",
      signOut: "Sign out",
      signingOut: "Signing out…",
      signIn: "Sign in",
      retry: "Try again",
    },
    common: {
      required: "This field is required.",
      backHome: "Back to courses",
    },
  },
  hero: {
    eyebrow: "University courses · Kuwait",
    titleLead: "Graduate with",
    titleAccent: "excellence.",
    subtitle:
      "Your courses, your pace, your language. Real lectures, notes, and labs — with instructors who stay with you after you enroll.",
    trustAria: "What every Gradex course includes",
    trust: [
      "Arabic & English",
      "Labs in every course",
      "Fair KWD pricing",
      "Instructor follow-up",
    ],
    cardTitle: "Intro to programming",
    cardMeta: "42 lessons · Beginner",
  },
  courses: {
    eyebrow: "Courses",
    title: "Start where your semester is.",
    subtitle:
      "A focused launch catalog for first-year computer science. Every course ships with notes and a hands-on lab.",
    browseAll: "Browse all courses",
    view: "View",
    labsIncluded: "Labs included",
    new: "New",
    lessons: "lessons",
    levels: {
      beginner: "Beginner",
      intermediate: "Intermediate",
      advanced: "Advanced",
    },
    emptyTitle: "Our first courses land soon",
    emptyBody:
      "We're finishing the launch catalog. Create an account and we'll tell you the moment a course goes live.",
    emptyAction: "Create account",
  },
  why: {
    eyebrow: "Why Gradex",
    title: "Built to get you through the semester — and past it.",
    pillars: [
      {
        title: "Labs, not just lectures",
        body: "Every course ends in a downloadable project with a guide. You finish able to build the thing, not just recognize it on an exam.",
      },
      {
        title: "A community that answers",
        body: "Enrol and you join the course community the same day. Ask, compare, and get unstuck alongside people taking the exact same course.",
      },
      {
        title: "Follow-up that shows up",
        body: "Support doesn't end at checkout. Instructors and the community stay reachable while you work through the course.",
      },
    ],
    note: "Fair price, not the cheapest — we compete on what you can build, never a race to the bottom.",
  },
  learn: {
    eyebrow: "How it works",
    title: "How a Gradex course works",
    subtitle:
      "Four steps, in order. Each one builds on the last — watching is where you start, not where you stop.",
    steps: [
      {
        title: "Watch the lecture",
        body: "Clear, exam-focused videos. Resume from where you left off, on any device.",
      },
      {
        title: "Study the notes",
        body: "Download slides and notes that get to the point — made to revise from, not to pad the runtime.",
      },
      {
        title: "Build the lab",
        body: "Download the project files and guide, and build something real with what you just learned.",
      },
      {
        title: "Ask in the community",
        body: "Stuck on the lab? Ask in the course community and keep the follow-up going until it clicks.",
      },
    ],
  },
  instructor: {
    eyebrow: "Instructors",
    title: "Learn from instructors who stay.",
    body1:
      "Gradex instructors are working lecturers and engineers from the region — teaching the courses they actually know, in the language you actually study in.",
    body2:
      "They don't disappear after you enrol. Follow-up in the course community is part of the course — not an upsell.",
    cta: "Meet the instructors",
    name: "Dr. Sara Al-Mutairi",
    role: "CS Lecturer · Kuwait University",
    quote:
      "“I teach the way I wish someone had taught me in first year — build first, and never leave a student stuck alone.”",
    creds: ["10+ years teaching", "Data structures", "Algorithms"],
    stats: [
      { value: "3", label: "courses" },
      { value: "120+", label: "lessons" },
      { value: "AR·EN", label: "languages" },
    ],
  },
  testimonials: {
    eyebrow: "Students",
    title: "What early students tell us",
    subtitle:
      "Voices from our pilot cohort. We'll only ever show reviews from students who actually took the course.",
  },
  faq: {
    eyebrow: "FAQ",
    title: "Questions, answered.",
  },
  finalCta: {
    title: "Ready to graduate with excellence?",
    body: "Browse the launch catalogue, or create a free account and pick up where your semester is.",
    browse: "Browse courses",
    register: "Create free account",
  },
  footer: {
    tagline:
      "Graduate with excellence. University courses for GCC students — real labs, real community, real follow-up.",
    explore: "Explore",
    company: "Company",
    legal: "Legal",
    links: {
      about: "About",
      teach: "Teach on Gradex",
      contact: "Contact",
      terms: "Terms",
      privacy: "Privacy",
      refund: "Refund policy",
    },
    copyright: "© 2026 Gradex. Built in Kuwait.",
    pricingNote: "Prices in KWD, VAT where applicable.",
    social: { discord: "Discord", x: "X", instagram: "Instagram" },
  },
};

export type Dictionary = typeof en;
