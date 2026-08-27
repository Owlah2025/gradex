export const en = {
  meta: {
    // Mirrors the Arabic counterpart exactly ("التبديل إلى الإنجليزية" = "Switch to English"),
    // so the two languages name the same control the same way.
    switchToAria: "Switch to Arabic",
    themeToggleAria: "Toggle dark mode",
    openMenu: "Open menu",
    closeMenu: "Close menu",
    skipToContent: "Skip to content",
    logoHomeAria: "Gradex home",
  },
  nav: {
    courses: "Courses",
    why: "Why Gradex",
    faq: "FAQ",
    login: "Log in",
    register: "Create account",
    browse: "Browse courses",
    dashboard: "Go to dashboard",
    instructorStudio: "Instructor Studio",
    adminWorkspace: "Admin workspace",
    workspaceNavigation: "Workspace navigation",
    courseReview: "Course review & administration",
    adminCourses: "Courses",
    academicCatalog: "Academic Catalog",
    courseAccess: "Course Access",
    courseLifecycle: "Course Lifecycle",
    reportedContent: "Reported Content",
    staffOperations: "Staff operations",
    courseBuilder: "Course Builder",
    notifications: "Notifications",
  },
  adminCourses: {
    title: "Courses",
    intro:
      "Every course on Gradex, with the ones waiting on you first. Open a course to review it — you never need its identifier.",
    filterGroupLabel: "Filter courses by state",
    capped:
      "Showing the most recently updated courses. Courses awaiting review are always listed in full; search by title to find any other course that is not shown.",
    searchLabel: "Search courses by title",
    searchPlaceholder: "Search by course title",
    searchSubmit: "Search",
    refresh: "Refresh",
    loading: "Loading courses…",
    loadFailed: "The course list could not be loaded.",
    retry: "Try again",
    queueUnavailable:
      "Course states are shown, but the review queue could not be read, so “Needs review” may be incomplete.",
    instructor: "Instructor",
    lastUpdated: "Last updated",
    submitted: "Submitted",
    revision: "Revision",
    firstPublication: "First publication",
    updatedRevision: "Update to a published course",
    resultCount: "courses",
    filters: {
      NEEDS_REVIEW: "Needs review",
      DRAFT: "Draft",
      CHANGES_REQUESTED: "Changes requested",
      PUBLISHED: "Published",
      WITHDRAWN: "Withdrawn",
      ALL: "All courses",
    },
    status: {
      DRAFT: "Draft",
      PENDING_REVIEW: "Submitted for review",
      CHANGES_REQUESTED: "Changes requested",
      PUBLISHED: "Published",
      DELISTED: "Delisted",
      ARCHIVED: "Archived",
    },
    // What each state means for the reader, in place of the raw enum.
    explain: {
      DRAFT:
        "The instructor is still building this course. It has not been submitted, and nothing is required from you yet.",
      PENDING_REVIEW: "This course has been submitted and is waiting for a review decision.",
      CHANGES_REQUESTED:
        "You returned this course to the instructor. It comes back here once they resubmit it.",
      PUBLISHED: "This course is live and can be granted to students.",
      DELISTED: "This course has been withdrawn from the public catalogue. Existing access is unaffected.",
      ARCHIVED: "This course has been archived.",
    },
    awaiting: {
      ADMIN: "Waiting on you",
      INSTRUCTOR: "Waiting on the instructor",
      NOBODY: "No action needed",
    },
    flags: {
      accessSuspended: "Access suspended",
      retired: "Retired",
    },
    actions: {
      REVIEW: "Review",
      MANAGE: "Manage",
      VIEW: "View",
    },
    empty: {
      NEEDS_REVIEW: "No courses are waiting for a review decision.",
      DRAFT: "No courses are currently being drafted.",
      CHANGES_REQUESTED: "No courses are back with an instructor for changes.",
      PUBLISHED: "No courses have been published yet.",
      WITHDRAWN: "No courses have been delisted, retired or archived.",
      ALL: "No courses exist yet. One appears here as soon as an instructor creates it.",
    },
    emptySearch: "No courses match this search.",
    clearSearch: "Clear search",
  },
  adminReview: {
    backToCourses: "Back to courses",
    breadcrumb: "Courses",
    loading: "Opening the review workspace…",
    notPending: {
      title: "This course is not waiting for a review decision",
      body: "It may have been decided already, or the instructor has not submitted it yet. The course list shows its current state.",
    },
    loadFailed: "The review workspace could not be opened.",
    retry: "Try again",
    reviewedNotice: "Decision recorded.",
    // Named next to the decision controls so the blocker is visible before Approve is pressed,
    // rather than only as a refusal afterwards.
    priceRequired:
      "A launch price is required before this course can be approved. Set one in the pricing section above.",
    priceReady: "A launch price is set.",
    /**
     * The submitted-revision inspector: the body of this workspace, and the only place a review
     * decision is made.
     *
     * Every string that used to be an `isAr ?` ternary inside the component lives here. That is not
     * tidying — the component had accumulated a third Arabic word for Course ("الكورس", a
     * transliteration, beside مقرر and دورة) precisely because its copy was never anywhere the
     * vocabulary guard could see it.
     */
    inspector: {
      title: "The submitted version",
      lead: "Everything below is the version the instructor submitted, not their current draft.",
      close: "Close",
      loading: "Loading the submitted version…",
      loadFailed: "The submitted version could not be loaded.",
      unavailable: "There is no valid submitted version to review.",
      details: "What was submitted",
      titleAr: "Title (Arabic)",
      titleEn: "Title (English)",
      descriptionAr: "Description (Arabic)",
      descriptionEn: "Description (English)",
      state: "Review state",
      studyYear: "Study year",
      major: "Major",
      subject: "Subject",
      notSpecified: "Not specified",
      unavailableTerm: "No longer in the catalogue",
      preview: "Public preview",
      previewPresent: "A separate public preview is attached to this version.",
      previewAbsent: "No public preview is attached to this version.",
      outline: "Sections and lessons",
      outlineEmpty: "This version has no sections.",
      section: "Section",
      lesson: "Lesson",
      media: "Video",
      previewLesson: "Preview the protected video",
      previewHeading: "Lesson preview",
      previewFailed: "The lesson preview could not be started.",
      taxonomyFailed: "The catalogue terms could not be loaded.",
      resource: "Resource",
      labMaterial: "Lab material",
      mediaState: {
        LOADING: "Checking…",
        READY: "Ready to preview",
        PROCESSING: "Being prepared",
        SCAN_PASSED: "Waiting to be prepared",
        FAILED: "Could not be prepared",
        QUARANTINED: "Withheld after scanning",
        UNAVAILABLE: "State unavailable",
        NO_VIDEO: "No video attached",
      },
      decision: "Your decision",
      approve: "Approve and publish",
      approveTitle: "Publish this course?",
      approveBody:
        "It becomes visible in the public catalogue and students can be granted access to it. The instructor can no longer edit this version.",
      approveConfirm: "Publish it",
      approved: "The course is published.",
      requestChanges: "Request changes",
      requestChangesTitle: "Send this back to the instructor?",
      requestChangesBody:
        "The course leaves your queue and returns to the instructor to edit. Nothing is published, and they can submit it again.",
      requestChangesConfirm: "Send it back",
      reason: "What needs to change",
      reasonHint: "The instructor reads this. Be specific about what to fix.",
      requested: "The change request was sent to the instructor.",
      cancel: "Cancel",
      failed: "The decision could not be recorded.",
      mismatch: "The loaded review did not match the course and version this workspace is for.",
      previewMismatch: "The issued video preview did not match the submitted lesson.",
      csrfMissing: "This session is missing its security token. Reload the page.",
      priceFirst:
        "Set the course price in the pricing section below before approving and publishing.",
    },
  },
  adminReviewQueue: {
    title: "Course review & administration",
    intro:
      "Courses an instructor has submitted, waiting on a review decision. Catalogue vocabulary is managed further down this page.",
    refresh: "Refresh",
    pendingCount: "awaiting a decision",
    tableCaption: "Courses awaiting a review decision",
    loading: "Loading the review queue…",
    loadFailed: "The review queue could not be loaded.",
    retry: "Try again",
    emptyTitle: "No courses are waiting for a review decision",
    emptyBody:
      "A course appears here as soon as an instructor submits it. Nothing is required from you until then.",
    course: "Course",
    revision: "Revision",
    publishType: "Publish type",
    submitted: "Submitted",
    actions: "Actions",
    firstPublication: "First publication",
    pendingRevision: "Update to a published course",
    unknownDate: "—",
    open: "Open review workspace",
    openFor: "Open the review workspace for",
  },
  adminLifecycle: {
    title: "Course lifecycle",
    intro:
      "Withdraw a course from the catalogue, put it back, retire it, archive it, or suspend access to it. Deletion is intentionally absent: a course students already hold access to is archived, never removed.",
    searchLabel: "Search by course title",
    searchPlaceholder: "Search by course title",
    searchSubmit: "Search",
    clearSearch: "Clear search",
    loading: "Loading courses…",
    loadFailed: "Courses could not be loaded.",
    retry: "Try again",
    emptyTitle: "No courses match this search",
    emptyBody: "Clear the search to list every course again.",
    tableCaption: "Courses and their current state",
    course: "Course",
    state: "State",
    actions: "Actions",
    manage: "Manage",
    manageFor: "Manage",
    selected: "Selected course",
    working: "Working…",
    actionFailed: "The action could not be completed.",
    failed: "Operation failed",
    reasonRequired: "A reason is required for this action.",
    csrfMissing: "Session CSRF token is missing",
    visibility: {
      title: "Catalogue visibility",
      body: "Delisting hides the course from the public catalogue. Students who already have access keep it, and the course can be relisted.",
      delist: "Delist",
      relist: "Relist",
    },
    withdrawal: {
      title: "Withdrawal",
      body: "Retiring stops new access to the course. Archiving is terminal — an archived course cannot be relisted, retired or archived again.",
      retire: "Retire",
      archive: "Archive",
    },
    suspension: {
      title: "Emergency access suspension",
      body: "Suspension denies every read of the course, including for students who hold access, until it is restored. It does not revoke anyone's access and does not change the course state.",
      causeLabel: "Suspension cause",
      reasonLabel: "Suspension or restoration reason",
      reasonPlaceholder: "Why access is being suspended or restored",
      suspend: "Suspend access",
      restore: "Restore access",
      causes: {
        LEGAL: "Legal",
        SECURITY: "Security",
        MALWARE: "Malware",
        SEVERE_MODERATION: "Severe moderation",
      },
    },
    completed: {
      delist: "Delist completed",
      relist: "Relist completed",
      retire: "Retire completed",
      archive: "Archive completed",
      suspend: "Access suspension completed",
      restore: "Access restoration completed",
    },
  },
  /**
   * The staff workspace: the people who can author and administer Courses, and the invitations that
   * are still waiting for an answer.
   *
   * Every consequence sentence here is the contract's, not a guess. Suspending an account revokes
   * whatever the person is currently signed in to and bumps their session epoch, and touches
   * nothing else — no Course changes state, no Student loses access. Cancelling an invitation moves it to REVOKED, which is what
   * stops the link in the invitee's mail from working. Re-inviting an address supersedes the open
   * invitation on the server, which is why there is no separate "resend".
   */
  adminStaff: {
    title: "Staff",
    intro:
      "The people who can author and administer courses on Gradex, and the invitations still waiting for an answer.",
    invite: {
      title: "Invite an instructor",
      lead: "They receive a one-time link by email and choose their own name and password.",
      email: "Email address",
      emailHint: "The address the invitation is sent to. It becomes their sign-in address.",
      role: "Role",
      roleFixed: "Instructor",
      roleNote: "Instructor is the only role that can be invited from here.",
      submit: "Send invitation",
      sending: "Sending the invitation…",
      success: "The invitation was sent.",
      failed: "The invitation could not be sent.",
      supersedes:
        "Inviting an address that already has an invitation waiting replaces it: the older link stops working and a new one is sent.",
    },
    invitations: {
      title: "Invitations waiting",
      lead: "Everyone who has been invited and has not yet created their account.",
      caption: "Staff invitations that are still open",
      invitee: "Invited",
      role: "Role",
      sent: "Invited on",
      actions: "Action",
      loading: "Loading invitations…",
      loadFailed: "The invitations could not be loaded.",
      retry: "Try again",
      emptyTitle: "Nothing is waiting",
      emptyBody:
        "Everyone who was invited has answered. This is a normal state, not a problem.",
      cancel: "Cancel invitation",
      cancelling: "Cancelling…",
      cancelTitle: "Cancel this invitation?",
      cancelBody:
        "The link in their email stops working immediately, and they cannot create an account until you invite them again.",
      cancelConfirm: "Cancel the invitation",
      keep: "Keep it",
      cancelled: "The invitation was cancelled.",
      cancelFailed: "The invitation could not be cancelled.",
    },
    instructors: {
      title: "Instructor accounts",
      lead: "Everyone who has completed their invitation.",
      caption: "Instructor accounts and whether each can sign in",
      instructor: "Instructor",
      email: "Email",
      state: "Account",
      actions: "Action",
      loading: "Loading instructor accounts…",
      loadFailed: "The instructor accounts could not be loaded.",
      retry: "Try again",
      emptyTitle: "No instructor accounts yet",
      emptyBody: "An account appears here once an invited instructor completes their invitation.",
      active: "Active",
      activeDetail: "Can sign in and work on their courses.",
      suspended: "Suspended",
      suspendedDetail: "Cannot sign in. Their published courses are unaffected.",
      suspend: "Suspend",
      suspending: "Suspending…",
      reinstate: "Reinstate",
      reinstating: "Reinstating…",
      reason: "Reason",
      suspendReasonHint: "Recorded with the suspension. Required.",
      reinstateReasonHint: "Recorded with the reinstatement. Required.",
      suspendTitle: "Suspend this instructor?",
      suspendBody:
        "They are signed out everywhere immediately and cannot sign in again until you reinstate them. Their published courses stay published and students keep their access.",
      suspendConfirm: "Suspend the account",
      reinstateTitle: "Reinstate this instructor?",
      reinstateBody: "They can sign in again and continue working on their courses.",
      reinstateConfirm: "Reinstate the account",
      keep: "Cancel",
      suspendSuccess: "The account was suspended.",
      reinstateSuccess: "The account was reinstated.",
      failed: "The account could not be changed.",
    },
  },
  /**
   * Course access operations.
   *
   * Gradex sells access out of band: a Student asks, pays somewhere the product does not see, and an
   * Administrator grants. Every word here is chosen to keep that honest — nothing on this screen
   * settles a payment, and "Confirm payment" means "I have seen the money arrive elsewhere", which
   * is why it is worded as a record of a decision rather than as a transaction.
   *
   * The states are the contract's five invitation states, three entitlement states and four
   * purchase-request states, each said as what it means for the two people involved rather than as
   * the enum that carries it.
   */
  adminAccess: {
    title: "Course access",
    intro:
      "Issue access to a published course, decide the requests waiting on you, and manage access that has already been granted.",
    refresh: "Refresh",
    genericFailure: "That could not be completed.",
    purchases: {
      title: "Purchase requests",
      lead: "Students who asked to buy a course. Payment happens outside Gradex; this is where you record what happened.",
      caption: "Purchase requests and what each is waiting for",
      searchLabel: "Search purchase requests",
      searchPlaceholder: "Reference, email, or course",
      search: "Search",
      reference: "Reference",
      student: "Student",
      course: "Course",
      price: "Price",
      requested: "Requested",
      state: "State",
      actions: "Action",
      loading: "Loading purchase requests…",
      loadFailed: "The purchase requests could not be loaded.",
      retry: "Try again",
      emptyTitle: "No purchase requests",
      emptyBody: "Nothing is waiting for you here. This is a normal state, not a problem.",
      emptySearchTitle: "No requests match that search",
      emptySearchBody: "Clear the search to see every request again.",
      status: {
        WAITING_PAYMENT: "Waiting for payment",
        INVITATION_CREATED: "Invitation sent",
        ACCESS_GRANTED: "Access granted",
        CANCELLED: "Cancelled",
      },
      statusDetail: {
        WAITING_PAYMENT: "You confirm the payment once it has arrived outside Gradex.",
        INVITATION_CREATED: "The student has been invited and has not accepted yet.",
        ACCESS_GRANTED: "Nothing further is needed.",
        CANCELLED: "This request was withdrawn.",
      },
      confirm: "Confirm payment & send invitation",
      confirming: "Confirming…",
      confirmTitle: "Record this payment as received?",
      confirmBody:
        "An invitation email goes to the student immediately. Gradex does not take or verify money — confirm only if the payment has actually arrived.",
      confirmAccept: "Yes, it arrived",
      confirmed: "The payment was recorded and the invitation sent.",
      cancel: "Cancel request",
      cancelling: "Cancelling…",
      cancelTitle: "Cancel this purchase request?",
      cancelBody:
        "The request is closed and no access is granted. Any invitation already sent for it stops working. The student would have to ask again.",
      cancelAccept: "Cancel the request",
      keep: "Keep it",
      cancelled: "The purchase request was cancelled.",
    },
    course: {
      title: "Which course",
      lead: "Every operation below applies to the course chosen here.",
      none: "Choose a published course to continue.",
    },
    expiry: {
      title: "Default access period",
      lead: "How long new access to this course lasts. Existing access is not affected.",
      appliesTo: "Applies to",
      date: "Access ends on",
      reason: "Reason",
      reasonHint: "Recorded with the change. Required.",
      reasonPlaceholder: "Standard 30-day cohort access",
      submit: "Save default",
      saving: "Saving…",
      saved: "The default access period was saved.",
      failed: "The default access period could not be saved.",
    },
    invite: {
      title: "Grant access to a student",
      lead: "They receive an invitation by email and accept it themselves. You approve it afterwards.",
      email: "Student email",
      emailHint: "The address the invitation is sent to.",
      note: "Internal note",
      noteHint: "Only staff see this. Optional.",
      notePlaceholder: "Approved under the scholarship programme",
      reference: "External reference",
      referenceHint: "Your own record from outside Gradex. Optional.",
      referencePlaceholder: "SCHOLARSHIP-2026-08",
      submit: "Send invitation",
      sending: "Sending…",
      sent: "The invitation was sent.",
      failed: "The invitation could not be sent.",
    },
    queue: {
      title: "Access requests and grants",
      lead: "Everyone who has been invited to a course, and where each one stands.",
      caption: "Course access invitations and their current state",
      student: "Student",
      course: "Course",
      state: "State",
      when: "Last change",
      actions: "Action",
      loading: "Loading access requests…",
      loadFailed: "The access requests could not be loaded.",
      retry: "Try again",
      emptyTitle: "No access requests yet",
      emptyBody: "An invitation appears here as soon as you issue one.",
      bounded: "Showing the most recent {shown} of {total}. This is not the whole list.",
      complete: "{total} in total.",
      legendTitle: "What each state means",
      reason: "Reason",
      status: {
        PENDING_STUDENT_ACCEPTANCE: "Waiting for the student",
        PENDING_ADMIN_APPROVAL: "Waiting for you",
        APPROVED: "Access granted",
        REJECTED: "Declined",
        CANCELLED: "Cancelled",
      },
      statusDetail: {
        PENDING_STUDENT_ACCEPTANCE: "They have not accepted the emailed invitation yet.",
        PENDING_ADMIN_APPROVAL: "They accepted. Approve to grant access.",
        APPROVED: "They can open the course.",
        REJECTED: "You declined this request.",
        CANCELLED: "This invitation was withdrawn.",
      },
      approve: "Approve",
      approveTitle: "Grant access to this course?",
      approveBody:
        "The student can open the course straight away, for the default access period set for it. You can change or end that access afterwards.",
      approveAccept: "Grant access",
      approved: "Access was granted.",
      reject: "Decline",
      rejectTitle: "Decline this request?",
      rejectBody:
        "No access is granted and the student is told the reason you give below. They would have to be invited again.",
      rejectAccept: "Decline it",
      rejectReason: "Why",
      rejectReasonHint: "The student reads this.",
      rejected: "The request was declined.",
      resend: "Resend link",
      resendTitle: "Send a new invitation link?",
      resendBody:
        "A fresh link is emailed to the student and the previous one stops working. Use this when the first email did not arrive or has expired.",
      resendAccept: "Send a new link",
      resent: "A new invitation link was sent.",
      cancel: "Cancel invitation",
      cancelTitle: "Cancel this invitation?",
      cancelBody:
        "The emailed link stops working and no access is granted. The student would have to be invited again.",
      cancelAccept: "Cancel the invitation",
      cancelled: "The invitation was cancelled.",
      manage: "Manage access",
      opening: "Opening…",
      keep: "Keep it",
    },
    entitlement: {
      title: "Access record",
      close: "Close",
      course: "Course",
      state: "State",
      endsAt: "Access ends",
      originally: "Originally granted until",
      revokedOn: "Access was ended on",
      source: "Granted through",
      status: {
        ACTIVE: "Access is active",
        REVOKED: "Access was ended",
        EXPIRED: "Access has run out",
      },
      grantSource: {
        MANUAL_INVITATION: "An invitation you issued",
        PURCHASE_REQUEST: "A purchase request",
      },
      adjustTitle: "Change when access ends",
      adjustLead:
        "A later date extends access; an earlier one shortens it. A date already past ends access immediately and keeps the student's enrollment and progress.",
      adjustDate: "New end date",
      adjustReason: "Reason",
      adjustReasonHint: "Recorded with the change. Required.",
      adjustReasonPlaceholder: "Semester extended for the whole cohort",
      supportReference: "Support reference",
      supportReferenceHint: "Your own ticket number. Optional.",
      adjustSubmit: "Save the new date",
      adjustSaving: "Saving…",
      adjusted: "The access end date was changed.",
      adjustFailed: "The access end date could not be changed.",
      revokeTitle: "End this access",
      revokeLead: "Use this when access should stop before its end date.",
      revokeReason: "Reason",
      revokeReasonHint: "Recorded with the change. Required.",
      revokeReasonPlaceholder: "Access ended after a refund handled outside Gradex",
      revokeSubmit: "End access",
      revoking: "Ending…",
      revokeConfirmTitle: "End this student's access now?",
      revokeConfirmBody:
        "They lose access to the course immediately. Their enrollment, learning progress and access history are kept, and this cannot be undone — a new invitation would be needed.",
      revokeConfirmAccept: "End the access",
      revoked: "Access was ended. Enrollment and progress are kept.",
      revokeFailed: "Access could not be ended.",
      keep: "Keep access",
      terminalRevoked:
        "This access was ended. It is kept as history and can no longer be changed.",
      terminalExpired:
        "This access has run out. It is kept as history and can no longer be changed.",
      historyTitle: "Changes to this access",
      historyEmpty: "No changes have been made to this access.",
      historyWhen: "Changed",
      historyReason: "Reason",
      historyNewEnd: "New end date",
      loadFailed: "The access record could not be opened.",
    },
  },
  /**
   * Catalogue vocabulary — the Majors and Subjects the legacy taxonomy offers.
   *
   * Retire and delete are different operations with different consequences, and the contract is
   * what decides the wording. Retiring sets `retired_at`: the term stops being offered for new
   * courses, courses already carrying it keep it, and no route exists to bring it back. Deleting
   * is refused outright by the server if any course revision references the term, so it only ever
   * removes vocabulary nobody used.
   */
  adminTaxonomy: {
    title: "Catalogue vocabulary",
    lead: "The majors and subjects courses can be filed under, managed separately from any one course review.",
    panelTitle: "Majors and subjects",
    kind: "Kind",
    major: "Major",
    subject: "Subject",
    existing: "Existing term",
    choose: "Choose a term",
    labelAr: "Arabic name",
    labelEn: "English name",
    academicCode: "Official code",
    academicCodeHint: "The university's own code for this subject. Optional.",
    retiredBadge: "Retired",
    activeBadge: "In use",
    create: "Add",
    created: "The term was added.",
    rename: "Rename",
    renamed: "The term was renamed.",
    retire: "Retire",
    retired: "The term was retired.",
    remove: "Delete",
    removed: "The term was deleted.",
    working: "Working…",
    needLabels: "Both the Arabic and the English name are required.",
    needTermAndLabels: "Choose a term and enter both names.",
    needTerm: "Choose a term first.",
    csrfMissing: "This session is missing its security token. Reload the page.",
    failed: "The term could not be updated.",
    loadFailed: "The catalogue vocabulary could not be loaded.",
    retry: "Try again",
    empty: "No terms yet. Add the first one above.",
    retireTitle: "Retire this term?",
    retireBody:
      "It stops being offered when a course is filed, and courses already using it keep it. There is no way to bring it back from here.",
    retireConfirm: "Retire it",
    removeTitle: "Delete this term?",
    removeBody:
      "It is removed permanently. This works only if no course uses it — if one does, the server refuses and nothing changes.",
    removeConfirm: "Delete it",
    keep: "Cancel",
  },
  /**
   * The Admin academic catalogue: University, then the colleges and departments under it, then the
   * majors those own, the study plan for a major, and the subjects a plan is built from.
   *
   * The hierarchy is already product language and stays that way — no table names, no join
   * vocabulary. What moved here is where the words live: they were 53  ternaries inside the
   * component, which is exactly the shape that let a third Arabic word for Course grow unnoticed
   * elsewhere in this tranche.
   */
  adminCatalog: {
    heading: "Academic Catalog",
    intro: "Manage universities, colleges, departments, majors, study plans, and subjects.",
    university: "University",
    universities: "Universities",
    college: "College",
    department: "Department",
    serviceUnit: "Service unit",
    units: "Colleges & departments",
    programs: "Majors",
    curriculum: "Study plan",
    subjects: "Subjects",
    emptyCatalog: "No universities yet. Start by adding one.",
    emptyUnits: "No colleges or departments yet.",
    emptyPrograms: "No majors yet.",
    emptySubjects: "No subjects yet.",
    emptyCurriculum: "No study plan yet.",
    emptyMappings: "No subjects added to this plan yet.",
    add: "Add",
    addUniversity: "Add university",
    addUnit: "Add college or department",
    addProgram: "Add major",
    addCurriculum: "Create study plan",
    addSubject: "Add subject",
    mapSubject: "Add subject to plan",
    retire: "Retire",
    nameAr: "Arabic name",
    nameEn: "English name",
    titleAr: "Arabic subject title",
    titleEn: "English subject title",
    slug: "Short identifier",
    officialCode: "Official code",
    officialCodeHint: "For example: 0410-101",
    degreeKind: "Degree kind",
    countryCode: "Country",
    maxLevel: "Highest academic level",
    foundationStage: "Has a foundation stage",
    parentUnit: "Belongs to",
    noParent: "Directly under the university",
    owningUnit: "Owning unit",
    versionLabel: "Plan version",
    supersedeActive: "Replace the current plan",
    requirementKind: "Requirement type",
    recommendedLevel: "Recommended level",
    kind: "Kind",
    select: "Select",
    saving: "Saving...",
    retired: "Retired",
    active: "Active",
    superseded: "Superseded",
    duplicateSubject: "This subject already exists at this university:",
    loadFailed: "Unable to load the Academic Catalog",
    saveFailed: "Unable to save",
    searchSubjects: "Search by code or name",
    csrfMissing: "This session is missing its security token. Reload the page.",
    retireSubjectTitle: "Retire this subject?",
    retireSubjectBody:
      "It stops being offered when a new course is filed. Study plans that already include it keep it, and the subject stays on record.",
    retireSubjectConfirm: "Retire it",
    keep: "Cancel",
    requirement: {
      UNIVERSITY_REQUIREMENT: "University requirement",
      COLLEGE_REQUIREMENT: "College requirement",
      MAJOR_CORE: "Major core",
      MAJOR_ELECTIVE: "Major elective",
      SUPPORTING: "Supporting",
      FREE_ELECTIVE: "Free elective",
    },
  },
  adminReports: {
    title: "Reported Content",
    intro: "Review Student reports and record one clear outcome.",
    queueHeading: "Open reports",
    loading: "Loading reported content…",
    emptyTitle: "No open reports",
    emptyBody: "New Student reports will appear here when they need an Admin decision.",
    loadFailed: "The reported-content queue could not be loaded.",
    detailFailed: "The report detail could not be loaded.",
    retry: "Try again",
    inspect: "Inspect",
    reasonLabel: "Reason",
    reporter: "Reported by",
    explanationLabel: "Student explanation",
    noExplanation: "No explanation provided.",
    targetLabel: "Reported content",
    targetUnavailable: "Target currently unavailable",
    unavailableBody: "The report remains available for resolution even though its target cannot be opened.",
    submittedAt: "Submitted",
    currentState: "Current Course state",
    courseLabel: "Course",
    open: "Open",
    resolved: "Resolved",
    resolutionReason: "Resolution reason",
    resolutionHint: "Keep this short and factual; it is retained for Admin audit history.",
    dismiss: "Dismiss — no platform action",
    delist: "Delist Course and resolve",
    resolving: "Recording resolution…",
    resolvedMessage: "Resolution recorded.",
    conflict: "This report was already resolved. The first decision was kept.",
    actionFailed: "The report could not be resolved. Nothing was changed.",
    denied: "This Admin operation is not available to this account.",
    openLifecycle: "Open Course lifecycle",
    status: "Status",
    reasons: {
      broken_unavailable: "Broken or unavailable",
      inaccurate: "Inaccurate",
      inappropriate: "Inappropriate",
      suspected_copyright_violation: "Suspected copyright violation",
      other: "Other",
    },
    targets: {
      COURSE: "Course",
      LESSON: "Lesson",
      VIDEO: "Video",
      RESOURCE: "Resource",
      LAB_MATERIAL: "Lab material",
    },
    actions: {
      DISMISSED: "Dismissed",
      DELISTED: "Course delisted",
    },
    lifecycle: {
      DRAFT: "Draft",
      PENDING_REVIEW: "Pending review",
      CHANGES_REQUESTED: "Changes requested",
      PUBLISHED: "Published",
      DELISTED: "Delisted",
      ARCHIVED: "Archived",
    },
    accessSuspended: "Access suspended",
    retired: "Retired",
    unknownState: "Unavailable state",
  },
  auth: {
    shell: {
      privacy:
        "Your password and verification link stay in this browser flow only.",
      language: "Language",
      // One panel per audience. A staff invitee never registers or confirms an
      // email, and someone already signed in is not at the start of the Student
      // funnel — showing them all the same three steps, with the first one lit,
      // told each of them they were somewhere they were not.
      student: {
        eyebrow: "Student access",
        sideTitle: "Start with one clear next step.",
        sideBody:
          "Create your Student account, confirm your email, then sign in to start learning.",
        steps: ["Create account", "Confirm email", "Sign in"],
      },
      staff: {
        eyebrow: "Staff invitation",
        sideTitle: "Your invitation decides your role.",
        sideBody:
          "The role on your invitation is already set. Choose a password, then sign in with the invited address.",
        steps: ["Open your invitation", "Set your password", "Sign in"],
      },
      session: {
        eyebrow: "Account security",
        sideTitle: "One step, then straight back to work.",
        sideBody:
          "You are already signed in. Finish this and Gradex takes you where you were heading.",
        steps: [],
      },
    },
    register: {
      title: "Create your Student account",
      intro: "Use an email you can open now. We’ll ask you to confirm it next.",
      displayName: "Display name",
      displayHint:
        "2–50 Arabic or Latin characters. This can be changed later.",
      email: "Email address",
      password: "Password",
      passwordHint:
        "15–128 characters. Spaces are welcome; there are no symbol rules.",
      policiesLoading: "Loading the current terms…",
      policySetLabel: "Terms version",
      policyEffective: "in effect since",
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
      unavailable:
        "Verification requests are temporarily unavailable. Try again shortly.",
    },
    result: {
      title: "Confirming your email",
      intro: "Keep this page open while Gradex checks the one-time link.",
      checking: "Checking the verification link…",
      successTitle: "Email confirmed",
      successBody:
        "Your Student account is active. Sign in to start browsing and learning.",
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
      unavailable:
        "Password reset is temporarily unavailable. Try again shortly.",
      failed: "The request could not be completed. Try again shortly.",
      backToSignIn: "Back to sign in",
    },
    /**
     * The staff invitation acceptance screen.
     *
     * Its copy used to live in a private object inside the component, in both
     * languages, outside every dictionary guarantee — parity, vocabulary, and
     * the ability to see all of the product's prose in one place.
     *
     * The four negative states are named separately because the preview route
     * names them separately. Collapsing them into "invalid, expired, revoked or
     * already used" made the reader guess which of four different next actions
     * was theirs. None of this leaks anything: whoever holds the link is the
     * person the invitation was addressed to, and the state is about them.
     */
    staffInvitation: {
      title: "Complete your staff invitation",
      intro: "Check the role you have been given, then choose a password.",
      checking: "Checking your invitation…",
      role: "Your role",
      roleInstructor: "Instructor",
      roleAdmin: "Administrator",
      fixed: "This role comes from your invitation and cannot be changed here.",
      name: "Display name",
      password: "Password",
      confirm: "Confirm password",
      complete: "Create staff account",
      completing: "Creating your account…",
      mismatch: "Both password fields must match.",
      invalidName: "Enter a name using 2–50 Arabic or Latin characters.",
      invalidPassword: "Use between 15 and 128 characters.",
      failed: "Your account could not be created. Try again shortly.",
      doneTitle: "Your staff account is ready",
      doneBody: "Sign in with the email address your invitation was sent to.",
      signIn: "Sign in",
      consumedTitle: "This invitation has already been used",
      consumedBody:
        "An account was created from this link. If that was you, sign in instead.",
      expiredTitle: "This invitation has expired",
      expiredBody:
        "Invitations are valid for a limited time. Ask the administrator who invited you to send a new one.",
      revokedTitle: "This invitation was cancelled",
      revokedBody:
        "It is no longer usable. Contact the administrator who invited you if you still need access.",
      supersededTitle: "A newer invitation replaced this one",
      supersededBody:
        "Open the most recent invitation email you received and use the link there.",
      missingTitle: "This page needs an invitation link",
      missingBody:
        "Open the link from your invitation email rather than typing this address.",
      unavailableTitle: "Your invitation could not be checked",
      unavailableBody:
        "This is a temporary problem, not a problem with your invitation. Reload the page to try again.",
    },
    staff: {
      createTitle: "Invite Instructor",
      createIntro: "Send an invitation to a new instructor.",
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
      reinstateReason: "Reason for reinstatement",
      reinstateAction: "Reinstate Account",
      reinstating: "Reinstating account…",
      onboardTitle: "Complete Staff Onboarding",
      onboardIntro:
        "Set your display name and password to complete your account setup.",
      displayName: "Display name",
      password: "Password",
      completeOnboarding: "Complete Onboarding",
      completingOnboarding: "Completing onboarding…",
      accountId: "Account ID",
      inviteSuccess: "Invitation sent successfully.",
      suspendSuccess: "Account suspended.",
      reinstateSuccess: "Account reinstated.",
      instructorsTitle: "Instructors",
      noInstructors: "No Instructor accounts yet.",
      accountStatus: "Status",
      active: "Active",
      suspended: "Suspended",
      loading: "Loading Instructor accounts…",
      loadingFailed:
        "Instructor accounts could not be loaded. Try again shortly.",
    },
    resetPassword: {
      title: "Choose a new password",
      intro:
        "This link works once. After resetting, sign in with your new password.",
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
      unavailable:
        "Password reset is temporarily unavailable. Try again shortly.",
      failed: "The password could not be reset. Try again shortly.",
      successTitle: "Password reset",
      successBody:
        "Every signed-in device was signed out. Sign in with your new password to continue.",
      goToSignIn: "Go to sign in",
      missingToken: "This page needs a reset link. Request one to continue.",
      requestNew: "Request a new link",
    },
    passwordChange: {
      title: "Change your password",
      intro:
        "Your account needs a new password before you can continue. This takes one step.",
      requiredTitle: "A password change is required",
      requiredBody:
        "This account was created with a temporary password. Choose your own to unlock the rest of Gradex.",
      current: "Current password",
      next: "New password",
      confirm: "Confirm new password",
      submit: "Change password",
      submitting: "Changing password…",
      signedInAs: "Signed in as",
      mismatch: "Both new password fields must match.",
      weak: "Choose a longer password. Use at least 15 characters.",
      sameAsCurrent: "The new password must be different from the current one.",
      wrongCurrent: "The current password is incorrect.",
      // One message for too short, too long, already in use on this account,
      // and found in a known breach. The server does not say which rule
      // matched and this must not guess.
      rejected:
        "That password cannot be used. Choose a different one of at least 15 characters.",
      reauthenticate:
        "For your security, sign in again before changing your password.",
      signedOut: "Your session ended. Sign in again to change your password.",
      limited: "Too many attempts. Wait a little before trying again.",
      failed: "The password could not be changed. Try again shortly.",
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
      opensInNewTab: "opens in a new tab",
      currentStep: "you are here",
      showPassword: "Show password",
      hidePassword: "Hide password",
    },
  },
  hero: {
    eyebrow: "University courses · Kuwait",
    titleLead: "Graduate with",
    titleAccent: "excellence.",
    subtitle:
      "Browse published Course details in Arabic or English, then learn through authorized Course access.",
    trustAria: "What Gradex makes clear before you request access",
    trust: [
      "Arabic & English",
      "Published course details",
      "KWD prices when configured",
      "Authorized learning access",
    ],
  },
  courses: {
    eyebrow: "Courses",
    title: "Start where your semester is.",
    subtitle: "Browse the Courses currently published by Gradex.",
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
    emptyTitle: "No published courses yet",
    emptyBody: "Courses will appear here when Gradex publishes them.",
  },
  learning: {
    dashboardTitle: "Your learning",
    dashboardIntro: "Continue your enrolled courses from one place.",
    resumeHeading: "Continue learning",
    resumeStartHeading: "Start learning",
    resumeAction: "Continue",
    resumeStartAction: "Start",
    resumeLesson: "Lesson",
    pendingAccessTitle: "Course access",
    accessActionRequiredOne:
      "1 course invitation is waiting for you to accept.",
    accessActionRequiredMany:
      "course invitations are waiting for you to accept.",
    pendingAccessOne: "1 course is waiting for approval.",
    pendingAccessMany: "courses are waiting for approval.",
    pendingAccessAction: "View access status",
    courseHome: "Course home",
    courseOutline: "Course outline",
    lessonNavigation: "Lesson navigation",
    active: "Active access",
    expired: "Access expired",
    accessUntil: "Access until",
    progress: "Progress",
    completedLessons: "completed",
    emptyTitle: "No courses to show",
    emptyBody:
      "Courses appear here once your access has been granted. If you are expecting one, check your course access.",
    unavailableTitle: "Learning is unavailable",
    unavailableBody: "This learning content is not available right now.",
    openCourse: "Open course",
    previousLesson: "Previous lesson",
    nextLesson: "Next lesson",
    firstLesson: "First lesson",
    lastLesson: "Last lesson",
    positionSeconds: "seconds",
    noExpiry: "No expiry",
    completed: "Completed",
    notCompleted: "Not completed",
    materials: "Lesson materials",
    resources: "Resources",
    labMaterials: "Lab materials",
    resource: "Resource",
    labMaterial: "Lab material",
    openResource: "Open resource",
    openLabMaterial: "Open lab material",
    download: "Download",
    preparingDownload: "Preparing download…",
    downloadUnavailable: "This file cannot be downloaded right now. Try again.",
    myCourses: "My courses",
    learningNavigation: "Learning navigation",
    courseContents: "Course contents",
    closeCourseContents: "Close course contents",
    backToCourse: "Back to the course",
    currentLessonLabel: "You are here",
    lessonNotStarted: "Not started",
    lessonInProgress: "In progress",
    files: "files",
    activeDetail: "You can open every lesson.",
    expiredDetail: "These lessons can no longer be opened.",
    completionAutomatic:
      "A lesson completes on its own once you have watched almost all of it.",
    courseCompleteTitle: "You have finished this course",
    courseCompleteBody:
      "Every lesson here is complete. You can go back to any of them while your access lasts.",
    loadingCourses: "Loading your courses…",
    loadingCourse: "Loading the course…",
    loadingLesson: "Loading the lesson…",
    emptyAction: "Check your course access",
    reportAction: "Report",
    reportCourseAction: "Report this course",
    reportLessonAction: "Report this lesson",
    reportVideoAction: "Report this video",
    reportResourceAction: "Report this resource",
    reportLabMaterialAction: "Report this lab material",
    reportDialogTitle: "Report content",
    reportDialogDescription: "Tell us what is wrong with this content.",
    reportTargetLabel: "Content",
    reportTargetCourse: "Course",
    reportTargetLesson: "Lesson",
    reportTargetVideo: "Video",
    reportTargetResource: "Resource",
    reportTargetLabMaterial: "Lab material",
    reportReasonLabel: "Reason",
    reportReasonPlaceholder: "Choose a reason",
    reportReasonBrokenUnavailable: "Broken or unavailable",
    reportReasonInaccurate: "Inaccurate",
    reportReasonInappropriate: "Inappropriate",
    reportReasonSuspectedCopyrightViolation: "Suspected copyright violation",
    reportReasonOther: "Other",
    reportExplanationLabel: "Details",
    reportExplanationOptional: "Optional",
    reportExplanationRequired: "Required",
    reportReasonRequiredError: "Choose a reason.",
    reportExplanationRequiredError: "Add details for this reason.",
    reportSubmit: "Send report",
    reportSubmitting: "Sending…",
    reportCancel: "Cancel",
    reportClose: "Close",
    reportSuccessTitle: "Report received",
    reportSuccessBody: "Thank you. We have received your report.",
    reportDone: "Done",
    reportDuplicate: "You have already reported this content.",
    reportThrottled: "Too many report attempts. Try again later.",
    reportUnavailable:
      "This content cannot be reported right now. Reload the page and try again.",
    reportInvalid: "Check the form and try again.",
    reportUnexpected: "The report could not be sent. Try again.",
  },
  player: {
    loading: "Preparing lesson…",
    unavailable: "This lesson could not start.",
    video: "Lesson video",
    play: "Play",
    pause: "Pause",
    seek: "Seek",
    elapsed: "Elapsed",
    duration: "Duration",
    volume: "Volume",
    mute: "Mute",
    unmute: "Unmute",
    quality: "Quality",
    auto: "Auto",
    fullscreen: "Enter fullscreen",
    exitFullscreen: "Exit fullscreen",
  },
  why: {
    eyebrow: "Why Gradex",
    title: "Built to get you through the semester — and past it.",
    pillars: [
      {
        title: "Clear course details",
        body: "Published course pages show their authored titles, descriptions, and available course outline.",
      },
      {
        title: "Arabic and English",
        body: "The Gradex interface is available in Arabic and English while Course content remains as authored.",
      },
      {
        title: "Authorised access",
        body: "An Admin-approved Course Access Invitation grants access to the invited Course.",
      },
    ],
    note: "Published course prices are displayed in KWD when the Course has a configured price.",
  },
  learn: {
    eyebrow: "How it works",
    title: "How a Gradex course works",
    subtitle:
      "Three supported learning steps. Each one builds on the last — watching is where you start, not where you stop.",
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
    ],
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
    tagline: "Graduate with excellence. University courses for GCC students.",
    explore: "Explore",
    legal: "Legal",
    links: {
      terms: "Terms",
      privacy: "Privacy",
    },
    copyright: "© 2026 Gradex. Built in Kuwait.",
    pricingNote: "Prices in KWD, VAT where applicable.",
  },
  access: {
    title: "Course access",
    intro: "Invitations you have received and the courses you can open.",
    navLabel: "Course access",
    loading: "Loading your course access…",
    failed: "Your course access could not be loaded.",
    retry: "Try again",
    emptyTitle: "No course access yet",
    emptyBody:
      "When an administrator invites you to a course, it will appear here. Browse the catalogue to see what is available.",
    emptyAction: "Browse courses",
    goToCourse: "Go to course",
    accessUntil: "Access until",
    reasonLabel: "Reason given",
    /** State labels and explanations. Never render the wire enum to a Student. */
    state: {
      ACTION_REQUIRED: {
        label: "Action needed",
        body: "Open the invitation link sent to your email to accept this course.",
      },
      AWAITING_APPROVAL: {
        label: "Waiting for approval",
        body: "You have accepted. An administrator still has to approve it before the course opens — there is nothing more for you to do, and you do not need to keep the invitation email.",
      },
      ACTIVE: {
        label: "Access granted",
        body: "You can open this course now.",
      },
      ACCESS_ENDED: {
        label: "Access ended",
        body: "Your access to this course has ended. Contact an administrator if you need it again.",
      },
      REJECTED: {
        label: "Not approved",
        body: "An administrator did not approve this request.",
      },
      CANCELLED: {
        label: "Withdrawn",
        body: "This invitation was withdrawn and is no longer active.",
      },
      UNKNOWN: {
        label: "No access",
        body: "You do not have access to this course.",
      },
    },
    /** The access section on public Course Details. Concise; the Access page carries the detail. */
    courseDetails: {
      heading: "Access to this course",
      viewStatus: "View access status",
      /** Gradex takes no payment. This must never read as a purchase path. */
      howItWorks:
        "An administrator invites you to a course. Accepting the invitation records your request; access opens once an administrator approves it.",
      anonymous:
        "Sign in to see whether you already have access to this course.",
      signIn: "Sign in",
      noAccess:
        "You do not have access to this course yet. Access begins with an administrator invitation.",
      actionRequired:
        "You have an invitation for this course. Open the invitation link sent to your email to accept it.",
      awaitingApproval:
        "You have accepted. An administrator still has to approve it — you do not need to accept again.",
      active: "You have access to this course.",
      accessEnded: "Your access to this course has ended.",
      rejected: "An administrator did not approve access to this course.",
      cancelled: "The invitation for this course was withdrawn.",
      unavailable:
        "Your access status could not be loaded. The course details above are unaffected.",
      retry: "Try again",
    },
    purchase: {
      heading: "Buy this course",
      intro:
        "Share your email first, then continue the payment conversation on WhatsApp.",
      action: "I want to buy this course",
      email: "Email address",
      invalidEmail: "Enter a complete email address.",
      submit: "Continue to WhatsApp",
      submitting: "Saving your request…",
      failed:
        "Your request could not be saved. WhatsApp was not opened; try again.",
    },
    /** The invitation-link surface. */
    invitation: {
      heading: "Course invitation",
      accept: "Accept invitation",
      accepting: "Accepting…",
      acceptedTitle: "Invitation accepted",
      acceptedBody:
        "An administrator will review it. You can close this page — your status stays on the Course access page.",
      purchaseAcceptedBody:
        "Your invitation is accepted and course access is now active. Opening your course…",
      acceptNote:
        "Accept to continue. If Gradex has already confirmed payment, access starts immediately; otherwise an administrator reviews your acceptance.",
      missingToken:
        "This invitation link is incomplete. Ask an administrator to send it again.",
      // Reaching the page with an invitation that is no longer waiting on the
      // Student is an ordinary, non-destructive outcome — most often their own
      // second visit to a link they already used. The panel used to render its
      // heading and then nothing at all.
      alreadyAnsweredTitle: "You have already answered this invitation",
      alreadyAnsweredBody:
        "There is nothing more to do here. Where it stands now is in your course access list below.",
      expired:
        "This invitation link is no longer usable. Ask an administrator to send a new one.",
      notFound: "This invitation is not available to your account.",
      wrongState: "This invitation can no longer be accepted.",
      failed: "The invitation could not be accepted.",
    },
  },
  /**
   * The public Course Details page.
   *
   * Every label here describes a field the public catalogue contract actually returns. There is no
   * copy for ratings, reviews, enrolment counts, course duration, or learning outcomes, because
   * `GET /api/v1/catalog/courses/{idOrSlug}` carries none of them and a label with nothing true
   * behind it is worse than an absent section.
   */
  courseDetail: {
    taughtBy: "Taught by",
    aboutHeading: "About this course",
    academicHeading: "Where this course fits",
    academicLead:
      "The study plan this course belongs to, so you can tell at a glance whether it is meant for you.",
    university: "University",
    major: "Major",
    subject: "Subject",
    subjectCode: "Course code",
    level: "Academic level",
    audience: "Studied in these programs",
    sectionsLabel: "Sections",
    lessonsLabel: "Lessons",
    sectionNumber: "Section",
    showAllSections: "Show every section",
    showFewerSections: "Show fewer sections",
    emptyCurriculum: "The outline for this course has not been published yet.",
    previewHeading: "Course preview",
    previewLead:
      "A short excerpt the instructor published openly. The rest of the course opens once you have access.",
    instructorHeading: "Your instructor",
    instructorRole: "Course author",
    instructorNote:
      "This course was written and is taught by this instructor on Gradex.",
    accessRegion: "Access to this course",
    accessJump: "Access options",
    unavailableTitle: "This course is not available",
    unavailableBody:
      "It may have been withdrawn, or the link may be wrong. Browse the catalogue to find another course.",
    loadFailedTitle: "This course could not be loaded",
    loadFailedBody:
      "Something went wrong while reading the catalogue. Try again in a moment.",
    loading: "Loading this course…",
  },
  instructor: {
    studio: {
      title: "Course Authoring Studio",
      intro:
        "Build and manage your course drafts here. Nothing reaches students until you submit a revision and an administrator approves it.",
      newCourse: "New course",
      cancelNewCourse: "Cancel",
      actionFailed: "That could not be completed.",
    },
    /**
     * The Instructor's course directory vocabulary.
     *
     * `standing` is deliberately a label *and* a meaning. A pill on its own said only that a
     * word applied; it never said whether the Instructor had to do anything about it.
     */
    courses: {
      heading: "Your courses",
      loading: "Loading your courses…",
      untitled: "Untitled course",
      open: "Open",
      emptyTitle: "You have not created a course yet",
      emptyBody:
        "A course begins with the university and the subject it is taught for. Create one and it appears here.",
      emptyAction: "Create your first course",
      academicUnset: "University and subject not chosen yet",
      selectPrompt: "Choose a course to work on",
      selectPromptBody: "Pick one from the list to edit its details, build its curriculum, and submit it for review.",
    },
    standing: {
      actor: {
        INSTRUCTOR: "Your turn",
        ADMIN: "With an administrator",
        NOBODY: "Nothing to do",
      },
      DRAFT: {
        label: "Draft",
        meaning: "Not submitted yet. Nobody outside Gradex can see it.",
        action: "Continue building",
      },
      DRAFT_UPDATE: {
        label: "Draft update",
        meaning: "Students keep seeing the published course while you work on this update.",
        action: "Continue this update",
      },
      IN_REVIEW: {
        label: "In review",
        meaning: "An administrator is reviewing it. It cannot be edited until they decide.",
        action: "View submitted course",
      },
      CHANGES_REQUESTED: {
        label: "Changes requested",
        meaning: "An administrator sent it back with a reason. Address it, then submit again.",
        action: "See what to change",
      },
      PUBLISHED: {
        label: "Published",
        meaning: "Students can find and study this course.",
        action: "View course",
      },
      UNAVAILABLE: {
        label: "No open revision",
        meaning: "There is nothing to edit on this course right now.",
        action: "View course",
      },
    },
    /**
     * The launch price, which the Instructor does not own.
     *
     * The studio used to open with a panel titled "Official Server Prices (Read-only Server
     * State)" â a sentence written for whoever built the endpoint. It listed every owned Course
     * again, in a second selector beside the one that already existed, so the screen had two lists
     * of the same courses and only one of them opened anything.
     */
    price: {
      heading: "Price",
      adminOwned:
        "Gradex sets the launch price while reviewing your course. You do not need a price to submit it.",
      courseLabel: "Course price",
      sectionsLabel: "Price per section",
      unset: "An administrator sets this during review.",
    },
    details: {
      createTitle: "Create a new course",
      createAction: "Create course",
      creating: "Creating…",
      needsSubject: "Choose the university and subject first.",
      created: "Course created.",
      createdWithRequest: "Course draft created, and your subject request was sent for review.",
      detailsTitle: "Course details",
      detailsLead: "What this course is called, and how it is described to students.",
      saveAction: "Save details",
      saving: "Saving…",
      saved: "Course details saved.",
      titleAr: "Course title (Arabic)",
      titleEn: "Course title (English)",
      descriptionAr: "Description (Arabic)",
      descriptionEn: "Description (English)",
      studyYear: "Study year",
      studyYearUnset: "Not set",
      /**
       * Legacy study-year labels (D-093 §6). Only a LEGACY_TAXONOMY Course is ever asked for one,
       * but while it is asked the options must be words rather than `YEAR_1`.
       */
      studyYears: {
        PREP: "Preparatory year",
        YEAR_1: "First year",
        YEAR_2: "Second year",
        YEAR_3: "Third year",
        YEAR_4: "Fourth year",
      },
      subjectRequestTitle: "Request a missing subject",
      subjectRequestBody:
        "You can keep building the course, but it cannot be submitted until an administrator links it to an official subject.",
      subjectRequestCode: "Official code (optional)",
      subjectRequestTitleAr: "Official subject name (Arabic)",
      subjectRequestTitleEn: "Official subject name (English)",
      subjectRequestNote: "Context for the administrator (optional)",
      subjectRequestSent: "Your subject request was sent for review.",
    },
    /**
     * Curriculum vocabulary.
     *
     * `videoAttached` replaces a line that printed the asset-version UUID beside the words "Video
     * attached". Whether a lesson has its video is the whole question; which row in the media
     * table holds it is not something an instructor can act on.
     */
    curriculum: {
      title: "Curriculum",
      lead: "Sections hold lessons. Every lesson needs a video before the course can be submitted.",
      sectionCount: "Sections",
      lessonCount: "Lessons",
      emptyTitle: "No sections yet",
      emptyBody:
        "A course is built from sections, and each section holds its lessons. Add the first section to begin.",
      noLessons: "This section has no lessons yet.",
      addSection: "Add section",
      addSectionTitleAr: "Section title (Arabic)",
      addSectionTitleEn: "Section title (English)",
      addLesson: "Add lesson",
      addLessonTitleAr: "Lesson title (Arabic)",
      addLessonTitleEn: "Lesson title (English)",
      deleteSection: "Delete section",
      deleteLesson: "Delete lesson",
      videoAttached: "Video attached",
      videoMissing: "No video yet",
      labMaterials: "Lab materials",
      confirmDeleteSectionTitle: "Delete this section?",
      confirmDeleteSectionBody:
        "The section and every lesson inside it are removed, including any video already uploaded to those lessons. This cannot be undone.",
      confirmDeleteLessonTitle: "Delete this lesson?",
      confirmDeleteLessonBody:
        "The lesson is removed, along with the video and any resources attached to it. This cannot be undone.",
      confirmDelete: "Delete",
      cancel: "Cancel",
    },
    /**
     * Upload vocabulary, shared by the three media surfaces.
     *
     * The phases are the real ones the client passes through â there is no invented progress and
     * no state the server did not report. `UPLOADING` is the only phase with a percentage, because
     * it is the only one the browser can actually measure.
     */
    media: {
      phase: {
        IDLE: "No upload in progress",
        PREPARING: "Preparing",
        UPLOADING: "Uploading",
        PROCESSING: "Processing",
        CHECKING: "Checking the file",
        ATTACHING: "Attaching",
        READY: "Ready",
        FAILED: "Upload failed",
      },
      videoLabel: "Lesson video",
      videoHint: "MP4 video. Every lesson needs one before the course can be submitted.",
      videoSelect: "Choose an MP4 file",
      videoAttached: "Video attached to this lesson.",
      resourceLabel: "Lesson resource",
      resourceHint: "PDF or DOCX. Optional â attach handouts, problem sets or slides.",
      resourceSelect: "Choose a PDF or DOCX file",
      resourceAttached: "File attached to this lesson.",
      resourceRemoved: "File removed from this lesson.",
      attachedFiles: "Attached files",
      remove: "Remove",
      removing: "Removing…",
      retry: "Try again",
      csrfMissing: "Your session expired. Reload the page and sign in again.",
      preview: {
        title: "Public preview",
        description:
          "A separate short video students can watch before buying. It is not a lesson, and stays private until an administrator approves this revision.",
        selected: "A public preview is attached to this revision.",
        absent: "No public preview is attached to this revision.",
        choose: "Upload public preview",
        replace: "Replace public preview",
        remove: "Remove public preview",
        processing: "Preparing your public preview…",
        upload: "Uploading public preview",
        ready: "Public preview is ready for review.",
        removed: "The public preview was removed from this revision.",
        failed: "The public preview could not be updated. Try again.",
      },
    },
    /**
     * Submission: the checklist, the transition, and the server's refusals in words.
     *
     * `violation` maps the codes `catalog/validation.go` returns. The target that accompanies each
     * one on the wire â `lesson:<uuid>` â is dropped: the checklist above already names the same
     * objects by the titles the Instructor wrote.
     */
    submission: {
      title: "Ready to submit?",
      leadIncomplete: "These need finishing before this course can be submitted.",
      leadReady: "Everything the studio can check is done.",
      progress: "done",
      adminOwnsPrice:
        "You do not set the price. An administrator sets it while reviewing the course.",
      serverNote:
        "An administrator makes the final check, so they may still ask for changes.",
      submitAction: "Submit for review",
      submitting: "Submitting…",
      confirmTitle: "Submit this course for review?",
      confirmBody:
        "The course goes to an administrator and cannot be edited until they decide. They will either approve it or send it back with a reason.",
      confirmAccept: "Submit",
      confirmCancel: "Keep editing",
      submitted: "Submitted. An administrator will review it.",
      rejectedTitle: "This could not be submitted yet",
      requirement: {
        ACADEMIC_INSTITUTION: "Choose the university this course is taught at",
        ACADEMIC_SUBJECT: "Choose the subject this course teaches",
        LEGACY_MAJOR: "Choose the major this course belongs to",
        LEGACY_SUBJECT: "Choose the subject this course belongs to",
        LEGACY_STUDY_YEAR: "Choose the study year this course is for",
        SECTIONS: "Add at least one section",
        SECTION_LESSONS: "Give every section at least one lesson",
        LESSON_VIDEOS: "Upload a video for every lesson",
      },
      untitledSection: "Section",
      untitledLesson: "Lesson",
      offenders: "Still needed in:",
      offenderMore: "and more",
      violation: {
        COURSE_EMPTY: "The course needs at least one section with lessons in it.",
        SECTION_EMPTY: "Every section needs at least one lesson.",
        LESSON_VIDEO_MISSING: "Every lesson needs a video.",
        ASSET_VERSION_UNAVAILABLE:
          "One of the uploaded files is no longer available. Upload it again.",
        ACADEMIC_INSTITUTION_MISSING: "This course needs a university.",
        ACADEMIC_SUBJECT_MISSING: "This course needs a subject.",
        ACADEMIC_SUBJECT_UNAVAILABLE:
          "The chosen subject is no longer available at this university. Choose another.",
        ACADEMIC_SUBJECT_RETIRED:
          "The chosen subject is no longer offered, so a new course cannot be published under it.",
        ACADEMIC_AUDIENCE_TARGET_UNAVAILABLE:
          "One of the programs you chose is no longer linked to this subject. Review your program choices.",
        TAXONOMY_DIMENSION_MISSING: "The course classification is incomplete.",
        TAXONOMY_TERM_UNAVAILABLE:
          "Part of the course classification is no longer available. Choose it again.",
      },
    },
    /**
     * The standing block that heads a selected course, and the read-only view of a course that is
     * with an administrator.
     *
     * A pill said "In review" and the studio then rendered the full editable form beneath it, with
     * a live Submit button, for a revision the server would refuse every write to.
     */
    standingBanner: {
      whoActsNext: "Who acts next",
      nothingRequired: "Nothing is required from you right now.",
      editingClosed: "This course cannot be edited while it is being reviewed.",
      editingOpen: "You can keep editing until you submit.",
      studentsUnaffected: "Students keep seeing the published course meanwhile.",
    },
    submitted: {
      title: "Submitted for review",
      body:
        "An administrator has it. They will either approve it or send it back with a reason, and it appears here either way. Nothing is required from you until then.",
      whatWasSent: "What you submitted",
      sections: "Sections",
      lessons: "Lessons",
      preview: "Public preview",
      previewYes: "Attached",
      previewNo: "Not attached",
    },
    academic: {
      title: "Academic identity",
      lead: "The university and subject this course is taught for. Students find it through them.",
      institutionLabel: "University",
      subjectLabel: "Subject",
      unitLabel: "Department or faculty",
      codeLabel: "Course code",
      change: "Change subject",
      cancel: "Cancel",
      changed: "Course subject updated.",
      loading: "Loading subject details…",
      lockedPublished:
        "This course has been published. Its subject is part of what the course is, and cannot change.",
      lockedInReview: "The subject cannot change while an administrator is reviewing this course.",
      audienceCustomized: "Course audience customized.",
      audienceReset: "Automatic audience restored.",
      audience: {
        automatic: "Programs that see this course",
        customized: "Chosen programs",
        automaticNote: "Every program the subject belongs to.",
        customizedNote: "You narrowed this course to the programs listed.",
        empty:
          "No programs are linked to this subject yet. Students still reach the course through the subject itself.",
        customize: "Choose programs",
        edit: "Change programs",
        useAutomatic: "Use every program",
        save: "Save programs",
        cancel: "Cancel",
        legend: "Programs this course is for",
      },
    },
    /**
     * University â Subject selection. Deliberately two steps, not five: an Instructor knows the
     * subject they teach, and the Academic Catalog derives the college and department from it.
     */
    picker: {
      universityLabel: "University",
      universityPlaceholder: "Select a university",
      universityFailed: "Universities could not be loaded.",
      subjectLabel: "Subject",
      subjectSearchPlaceholder: "Search by subject name or code",
      searchFailed: "Subjects could not be searched.",
      searching: "Searching…",
      change: "Change subject",
      noMatch: "No matching subject. Try the subject code, or another name.",
      requestMissing: "I cannot find my subject",
      audienceTitle: "Programs this subject belongs to",
      audienceEmpty:
        "The catalog links no program to this subject yet. Students still reach the course through the subject itself.",
      level: "Level",
    },
    /** A subject the catalog does not carry yet, requested from the administrators who own it. */
    request: {
      loadFailed: "The status of your subject request could not be loaded.",
      sentButStale: "Your request was sent, but its status could not be refreshed. Reload to see it.",
      pendingTitle: "Subject request under review",
      pendingBody:
        "Keep building the course. It cannot be submitted until an administrator links it to an official subject.",
      rejectedTitle: "Subject request declined",
      open: "I cannot find my subject",
      code: "Official code (optional)",
      titleAr: "Subject name (Arabic)",
      titleEn: "Subject name (English)",
      note: "Note for the administrator (optional)",
      send: "Send request",
    },
    /**
     * Legacy taxonomy compatibility (D-093 Â§6), removed at T5.
     *
     * It stays reachable only while an Instructor still owns a LEGACY_TAXONOMY course, and is
     * deliberately named as the older way of classifying one rather than dressed up as current.
     */
    legacyTaxonomy: {
      title: "Explicit Draft Taxonomy",
      lead: "Saves to the displayed editable revision only; no latest-revision lookup is used.",
      courseLabel: "Course",
      coursePlaceholder: "Select a course",
      save: "Save Taxonomy",
      saving: "Saving…",
      saved: "Taxonomy saved for the named revision.",
      incomplete: "Select an editable draft, a major, and a subject.",
      loadFailed: "The classification vocabulary could not be loaded.",
      saveFailed: "The classification could not be saved.",
    },
    roster: {
      title: "Students",
      open: "View students",
      close: "Hide students",
      loading: "Loading students…",
      error: "Students could not be loaded. Try again shortly.",
      empty: "No students are enrolled in this course yet.",
      student: "Student",
      accessStatus: "Access status",
      joined: "Joined",
      accessStarted: "Access started",
      accessUntil: "Access until",
      unavailableDate: "—",
      previous: "Previous",
      next: "Next",
      page: "Page",
      statuses: {
        ACTIVE: "Active",
        EXPIRED: "Expired",
        REVOKED: "Revoked",
        SUSPENDED: "Access suspended",
        DENIED: "No current access",
      },
    },
    /**
     * Human labels for the revision lifecycle. The wire enum stays available as a data attribute
     * for tests and support; it is never the Instructor's primary explanation of what happened.
     */
    revisionState: {
      DRAFT: "Draft",
      PENDING_REVIEW: "In review",
      CHANGES_REQUESTED: "Changes requested",
      REJECTED: "Changes requested",
      APPROVED: "Approved",
      SUPERSEDED: "Superseded",
    },
    revision: {
      startTitle: "This course is published",
      startBody:
        "Students see the published version. To change it, start a new revision — the published version keeps serving until an administrator approves your changes.",
      startAction: "Start a new revision",
      starting: "Starting…",
      startFailed:
        "The revision could not be started. Nothing was changed — try again.",
      editingPublishedTitle: "You are editing a draft revision",
      editingPublishedBody:
        "Students still see the published version. Nothing here reaches them until you submit this revision and an administrator approves it.",
      inReviewTitle: "This revision is with an administrator",
      inReviewBody:
        "It cannot be edited while it is in review. The published version is unaffected.",
      unavailable: "This course has no editable revision.",
    },
    changeRequest: {
      title: "Changes requested before this course can be published",
      reasonLabel: "What the reviewer asked for",
      noReason:
        "The reviewer did not record a reason. Contact an administrator before resubmitting.",
      nextStep:
        "Edit the course below to address this, then choose Submit for review again. Your course stays editable until you resubmit.",
    },
  },
  academicContext: {
    eyebrow: "Personalize",
    title: "Which university are you studying at?",
    lead: "Tell us your university and program, and the catalogue will lead with the courses that belong to your study plan. You can change it whenever you like.",
    notAnAccount: "No account needed. Your choice stays on this device until you decide to sign up.",
    universityLabel: "University",
    programLabel: "Program",
    programOptional: "Optional",
    chooseUniversity: "Choose your university",
    chooseProgram: "Choose your program",
    anyProgram: "All programs at this university",
    submit: "Show my courses",
    skip: "Browse everything instead",
    loading: "Loading universities…",
    loadingPrograms: "Loading programs…",
    loadFailed: "Universities could not be loaded.",
    programsFailed: "Programs for this university could not be loaded.",
    retry: "Try again",
    noInstitutions: "No universities are listed yet.",
    noPrograms: "No programs are listed for this university yet. You can still browse its courses.",
    summaryTitle: "Your academic context",
    showingFor: "Showing courses for",
    savedOnDevice: "Saved on this device",
    profileBacked: "From your academic profile",
    change: "Change",
    changeAria: "Change your university or program",
    showAll: "Show all courses",
    emptyTitle: "No courses for this study plan yet",
    emptyBody: "Nothing has been published for this university and program so far. Change your selection, or browse the whole catalogue.",
    backToCatalogue: "Back to courses",
    handoffTitle: "Confirm your academic profile",
    handoffLead: "You were browsing Gradex as:",
    handoffNote: "That choice was only remembered on this device. Choose your university and major below to save it to your account.",
  },
  /**
   * The Student's own academic profile: the authenticated, account-backed one.
   *
   * Kept apart from `academicContext`, which is the browsing preference a
   * visitor can set without an account. The two are never the same fact and
   * this copy must never let them read as though they were.
   *
   * All of it used to live in a `copy(isAr)` function inside the form, which is
   * the one place two languages cannot be checked for parity, for the retired
   * vocabulary, or for simply existing.
   */
  academicProfile: {
    onboardingTitle: "Tell us about your studies",
    onboardingIntro:
      "We use this to order the catalogue around your studies. You can skip now and finish later.",
    editTitle: "Your academic profile",
    editIntro:
      "This shapes what the catalogue shows you first. Change it whenever your studies change.",
    university: "University",
    college: "College",
    program: "Major",
    level: "Academic level",
    levelUnsure: "I'm not sure",
    undeclared: "I haven't chosen my major yet",
    nonDegree: "Non-degree student",
    foundation: "I'm in the foundation year",
    select: "Select",
    selectCollegeFirst: "Choose your university first",
    selectProgramFirst: "Choose your college first",
    save: "Save and continue",
    saveEdit: "Save changes",
    skip: "Skip for now",
    saving: "Saving…",
    accessPromise:
      "Changing your major or level only changes how the catalogue is personalised. Your courses and purchases are unaffected.",
    saved: "Your academic profile was saved.",
    skipped: "Skipped. You can finish your profile any time.",
    loadFailed:
      "Your study options could not be loaded. Try again in a moment.",
    saveFailed:
      "Your academic profile could not be saved. Try again in a moment.",
    // Deliberately not "the CSRF token is missing", which is what this screen
    // used to say to a Student.
    sessionEnded: "Your session ended. Sign in again to save your profile.",
    noPrograms: "No majors are available for this college yet.",
    currentlyOn: "Your study plan",
    // The server keeps a setup state. The reader gets the consequence of it,
    // never the word the server uses.
    notStartedTitle: "You haven't set this up yet",
    notStartedBody:
      "Nothing here is required. Filling it in changes what the catalogue shows you first.",
    skippedTitle: "You skipped this earlier",
    skippedBody: "You can finish it now, and change it again later.",
    backToCourses: "Back to my courses",
  },
};

export type Dictionary = typeof en;
