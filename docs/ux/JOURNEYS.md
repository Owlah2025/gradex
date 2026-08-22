# Journey Maps and Friction

> Status: Student journey mapped against implementation. Instructor/Admin mapped at outline depth.
> Last Updated: 2026-08-19
> Derived from [USER_JOURNEYS.md](../USER_JOURNEYS.md) + [NAVIGATION_MAP.md](../NAVIGATION_MAP.md),
> then walked against `frontend/src/app/**`.

`✅` implemented · `⚠️` implemented with significant UX defects · `❌` missing · `🔌` blocked on backend

---

## SJ — The Student spine

```
 Landing ✅
    │
    ├─► Catalog ⚠️ ──► Course Details ⚠️ ──► "how do I get access?" ❌ DEAD END
    │                                              │
    │                                    (external, out of band)
    │                                              ▼
    │                                   Admin creates invitation
    │                                              │
    │                                        email deep link
    │                                              ▼
    ├─► Register ✅ ─► Verify ✅ ─┐        /[locale]/access ⚠️⚠️
    ├─► Login ✅ ────────────────┴──────────────►  │
    │                                          Accept ⚠️
    │                                              ▼
    │                                  Awaiting admin approval ⚠️
    │                                              │
    │                                     (no notification ❌)
    │                                              ▼
    └─► Dashboard ⚠️ ──► Course Home ⚠️ ──► Lesson Player ⚠️
              │                 │                  │
              │                 ├─► Resources/Labs ❌ (raw API navigation)
              │                 └─► Office hours ❌🔌
              ├─► Continue learning ❌🔌
              ├─► Access history ❌ (exists only on /access, unlinked)
              ├─► Notifications ❌🔌
              └─► Profile ❌🔌
```

### The three structural breaks

**Break 1 — the discovery-to-access chasm.**
The catalog and Course Details pages have no way to tell a Student how to get access. ST02 requires
"how-to-get-access guidance"; it is not implemented and no field carries it. A Student who wants a
Course reaches the end of Course Details and the journey stops. Access begins with an Admin action
the Student cannot trigger or discover from inside the product. Until this is designed, the Student
funnel does not connect. **This is the single most important Student UX problem in Gradex.**

**Break 2 — the invitation island.**
`/[locale]/access` is unreachable except via a one-time email link with a URL-fragment token. It has
no shell, no navigation, no link from the dashboard, and is not in any menu. A Student who closes the
tab mid-flow, or who wants to check "did my access come through?", has no route back. The page also
holds ST10 Access History, so the entire access-status surface is hidden behind a consumed email link.

**Break 3 — the shell vacuum.**
Once signed in, a Student on any `/learn/*` route has no header, no navigation, no way to browse, no
profile, and no sign-out. Every trip out of the learning area requires typing a URL or using browser
Back. There is no answer to "where am I" and no answer to "how do I get back".

### Journey-level friction

| # | Where | Problem | Why it hurts a first-time Student |
|---|---|---|---|
| J-01 | Course Details | No access path, no price context beyond a number, no `Go to Course` when already entitled | The evaluation step ends in nothing |
| J-02 | `/access` | Shows `Course ID: 3f2a…` and `PENDING_ADMIN_APPROVAL` | The Student is asked to reason about a UUID and a database enum |
| J-03 | `/access` | English-only | The default-locale Arabic Student cannot read the only screen that grants access |
| J-04 | `/access` → dashboard | No handoff after acceptance | Success message, then nowhere to go |
| J-05 | Awaiting approval | No notification, no status polling, no email-independent status route | The Student must guess when to check back |
| J-06 | Dashboard | No Continue learning; no pending-invitation surface | The dashboard's stated purpose (ST05) is unmet |
| J-07 | Dashboard → Course Home | Context lost: no breadcrumb, no course identity in the lesson header | Deep in a Course, the Student cannot see which Course |
| J-08 | Course Home | Back link at page bottom, labelled with the destination's title | Reads as a section heading, not a way back |
| J-09 | Lesson Player | No lesson rail | No sense of position or of what is next |
| J-10 | Lesson Player | Materials above the video | The primary object of the page is pushed below secondary actions |
| J-11 | Any learning failure | One message for 401/403/404/expired/5xx, no retry, no sign-in route | An expired session looks identical to a deleted Course |
| J-12 | Any navigation | No loading state on `force-dynamic` server pages | Clicking appears to do nothing |
| J-13 | Any mutation | No toast/confirmation primitive in the product | Feedback is ad-hoc inline text or absent |
| J-14 | Catalog | No filters, no result count, no pagination | Ranking and scope are invisible; unusable past a thin catalog |
| J-15 | Catalog → Details | The page chrome changes | Reads as leaving the site |
| J-16 | Expired access | Course Home renders the outline and silently removes material links | Removal is never explained |
| J-17 | Materials | Click → raw API navigation | Download failure has no surface |
| J-18 | Language switch | Three different toggles behaving differently; SSR always RTL | Switching feels unreliable |
| J-19 | Terminology | "Student Course Access Portal", "Watch Course", "Open & Watch Course", "Open Course" for the same action | No consistent verb for the core action |
| J-20 | Whole journey | No profile, no notifications, no way to change language persistently while signed in | The account has no home |

### Terminology to fix (single verb per action)

| Concept | Currently | Should be |
|---|---|---|
| Enter a Course | "Open Course" / "Watch Course" / "Open & Watch Course" / "Go to Course" | **Go to course** ([SCREENS.md](../SCREENS.md) ST02, BR-024) |
| Resume | — | **Continue learning** ([SCREENS.md](../SCREENS.md) ST05) |
| Access surface | "Student Course Access Portal" | **Course access** ([GLOSSARY.md](../GLOSSARY.md)) |
| Section | "Section" in code | "Section" canonical; "Chapter" allowed as UI copy only ([SCREENS.md](../SCREENS.md) conventions) |
| Invitation state | raw enums | localized labels via a `StatusBadge` map |

---

## IJ — Instructor (outline)

```
 Staff invitation email ✅ ─► /staff/accept ⚠️ ─► Login ✅ ─► Instructor Studio
                                                                   │
                                          ┌────────────────────────┤
                                    Dashboard ❌         Course Builder ⚠️
                                                                   ├─► Lesson editor ⚠️ (embedded)
                                                                   ├─► Materials ⚠️ (embedded)
                                                                   ├─► Public preview ❌
                                                                   ├─► Submit / review status ⚠️
                                                                   ├─► Analytics ❌
                                                                   └─► Office hours ❌🔌
```
Breaks: no dashboard, so lifecycle state and change-request responses have no home; the shell offers
2 of 6 specified destinations; duplicate `/instructor/*` and `/[locale]/instructor/*` route trees.

## AJ — Admin (outline)

```
 Login ✅ ─► Admin workspace
              ├─► Ops home ❌
              ├─► Course review + taxonomy + pricing ⚠️ (all merged into /admin/catalog)
              ├─► Course access invitations ⚠️
              ├─► Entitlement detail ❌
              ├─► Users/staff ⚠️ (at /staff, outside the locale tree)
              ├─► Reported content ❌  ← Students can file reports today with no resolution surface
              └─► Office-hours moderation ❌🔌
```
Breaks: no operations home; three specified destinations collapsed into one route; reports are
accepted by the product with nowhere to work them.
