# Batch D public-surface truthfulness design

## Scope

This design implements only the D-084 H1–H3 remediation tasks T040–T045 in
`specs/004-public-catalogue/tasks.md`. It does not add payments, checkout,
refunds, community features, public placeholder pages, or backend behavior.

## Landing catalogue

`FeaturedCourses` will call the existing published-only public catalogue client
used by `/[locale]/catalog`. The landing surface renders a bounded selection of
the returned Courses and links each Course to its locale-aware authoritative
catalogue detail route. It renders only returned title, taxonomy, instructor
display name, preview availability, and price when present; it does not invent
course codes, ratings, counts, durations, thumbnails, or instructor details.

Loading, empty, and failed reads remain distinct. An empty response does not
fall back to fixtures, and a failed response is not presented as an empty
catalogue.

## Links and public copy

Public route helpers will create locale-aware catalogue and Student dashboard
paths. The unavailable company destinations (`/about`, `/teach`, `/contact`)
will be removed from the footer instead of receiving placeholder pages.

The FAQ will describe D-045 accurately: payment is arranged externally, an
Admin creates and approves a Course Access Invitation, and Admin approval
creates access. It will make no Tap checkout, in-platform payment, refund, or
community claim.

## Removed content

The landing Testimonials section and fabricated testimonial data are removed
entirely. No quotes, names, institutions, ratings, statistics, or replacement
marketing claims are introduced.

## Verification

Focused unit/structural coverage and the existing public-catalogue Playwright
suite will prove authoritative catalogue rendering, honest empty/error states,
locale-aware links, absence of obsolete public links/testimonials, and absence
of deferred-commerce claims. The normal frontend typecheck, lint, test, and
production build remain required.
