# Landing Study-Plan Subject Discovery

**Date:** 2026-09-16

**Release baseline:** `af45f9adadc5b37e29126a5192f6bb48825817c7`

**Branch:** `landing-study-plan-subjects-20260916`

## Goal

The landing page must distinguish published GradeX Courses from academic Subjects that GradeX may
not teach yet. It will present published Courses under **Available now**, then a university- and
program-filtered Subject rail under **Courses for your study plan**, followed by the existing Course
Bundles and remaining landing sections.

Subjects and Courses remain separate domain concepts. No placeholder Course rows, catalogue
imports, migrations, or production-data mutations are part of this feature.

## Landing composition

The page order is:

1. Hero and navbar
2. Available now
3. Courses for your study plan
4. Course Bundles
5. Existing remaining sections

`FeaturedCourses` remains the presentation of the public Course catalogue. It always reads the
published-Course endpoint and renders only real Course data. It no longer changes its product
meaning based on the anonymous academic selection.

The new `StudyPlanSubjects` section owns landing-only Subject discovery state. The hero's existing
academic selection continues to write through `AcademicContextProvider`; after that selection
resolves, the existing landing journey scrolls to the study-plan section rather than the start of
the entire post-hero stack.

Course Bundles remain immediately after the study-plan section.

## Academic context and filters

The study-plan section initializes its browsing filters in this order:

1. A completed signed-in Student academic profile with institution and program slugs.
2. The existing anonymous institution/program selection held by `AcademicContextProvider`.
3. No selection, in which case compact University and Program selectors are shown.

Changing the landing filters updates only the anonymous browsing preference. It never writes the
Student academic profile. A profile-backed Student may change the displayed catalogue without
changing account data.

The section uses the existing public academic option endpoints for Institutions and Programs. A
University change clears the previous Program immediately, cancels stale Subject/Program requests,
and prevents late responses from replacing the newer selection.

The landing displays at most 12 Subjects. The public Subject query applies `institution`, `program`,
and optional `availability=served` on the server before counting, ordering, and pagination.

The **View all subjects** URL uses the existing `institution` and `program` query vocabulary. The
full `/[locale]/subjects` catalogue gains the same Program selector and server-side filtering, so
the link's filters remain visible, shareable, and functional.

## API changes

### Public Subject catalogue

`GET /api/v1/catalog/subjects` gains the optional `program` query parameter. The repository filters
Subjects through active curricula and active Programs from the existing academic taxonomy before
both the count and page queries. An unknown or retired Program yields an ordinary empty result.

Served/unserved remains backend-derived truth. `served` continues to be computed from eligible
published Courses using the repository's publication visibility policy. The frontend never infers
service status from the projected Course.

Each served Subject response gains an additive `primary_course` projection containing the real
public Course-card data. The repository selects it deterministically from the same eligible
published Course set and an explicit stable order. It then resolves all primary Course projections
for the page in one batched repository read, not one call per Subject. Existing `courses` references
remain backward-compatible for the full Subject catalogue and detail routes.

### Student academic profile

`GET /api/v1/me/academic-profile` gains additive `institution_slug`. It is read beside the existing
Institution name and identifier. Existing clients remain valid because no existing field changes
meaning or shape.

No database migration is required.

## Subject cards

A landing Subject card has the same proportions, border language, typography, and rail behavior as
the existing Course cards.

For an unserved Subject, the image region is deterministic CSS/SVG artwork derived from stable
Subject information. It uses restrained pastel variants, geometric outlines, and faded official
code typography. The information region shows the real code/title, **Not available yet**, and the
existing compact `SubjectDemandAction`. It never shows an instructor, price, thumbnail, or other
invented Course data.

For a served Subject, the card uses `primary_course` for its real thumbnail, Course title,
instructor, price, preview indication, and Course route. The backend `served` field decides which
state renders. The CTA is **View course** and the demand action is absent.

The rail shows up to 12 results with approximately four cards plus a neighbor edge on desktop,
two-to-three on tablet, and one-plus-neighbor on mobile. It uses the existing accessible carousel
controller and logical RTL behavior. A single item keeps a bounded card width instead of stretching
into unused space.

## Demand and authority

The landing reuses `SubjectDemandAction`, `subjectDemandAudience`, `/me/subject-demand`, CSRF token
handling, and `withReturnTo`.

- Anonymous visitors receive the existing login return path, including the landing query and
  `request=1` intent.
- Eligible Students create and withdraw their own demand through the existing endpoints; successful
  state is retained after reload by `listOwnSubjectDemand`.
- Admin, Instructor, restricted, and unresolved sessions receive no Student mutation authority.
  Backend authorization remains the authority; the frontend only avoids offering an action the
  backend will refuse.

## States and localization

The section explicitly handles context resolution, institution loading/failure, no University,
Program loading/failure, an Institution with no Programs, Subject loading/failure, no Subjects, one
Subject, many Subjects, served/unserved cards, request pending/requested, and authentication return.

English and Arabic copy includes headings, subtitles, selectors, availability filters, state
messages, actions, and carousel labels. Arabic remains `dir=rtl`; controls, rail progression, focus
states, and directional icons follow the existing locale provider and carousel conventions.

## Verification

Focused frontend tests cover Course-only Available-now behavior, filter transitions and stale
responses, every Subject state, real served Course data, absence of fake unserved data, request/auth
authority, and localized RTL-sensitive output. Backend integration tests cover server-side Program
filtering before pagination/counting, deterministic primary-Course selection, publication-policy
exclusions, and the additive profile slug.

The Playwright journey covers anonymous English and Arabic discovery, Student profile
initialization, disposable Student request persistence/withdrawal, Institution switching without
result leakage, role authority, and desktop/tablet/mobile rail behavior using content and action
assertions rather than screenshot-only checks.

Before deployment, the production release procedure must fail closed unless schema version 38 is
clean and catalogue counts remain 15 Institutions, 329 Subjects, and 84 Kuwait University Subjects.
The current site must also be healthy. Release packaging, artifact digest comparison, OCI revision
verification, deployment health checks, production browser smoke tests, catalogue recount, and log
inspection follow the repository's existing production tooling. No migration, import, catalogue
reset, demand-row creation, or R2 cleanup is authorized by this feature.
