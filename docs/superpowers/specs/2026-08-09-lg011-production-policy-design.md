# LG-011 production policy-set and legal pages design

Date: 2026-08-09  
Status: Product Owner approved for implementation  
Authoritative content: `docs/legal/lg011-approved-policy-package.md`

## Scope

Close the LG-011 software gap in the existing credential-admission boundary.
The work adds the approved bilingual policy set and public legal pages; it does
not add commerce, a standalone Refund Policy, or a second registration flow.
Terms section 8 remains the approved payment/consumer-rights disclosure, while
Privacy section 4 and Terms section 4 remain the course-access disclosures.

## Backend ownership

`config.Config` owns immutable legal identity settings and rejects invalid
non-development configuration before composition. Public deployments reject
the two approved staging sentinels. A narrowly typed controlled-staging mode is
accepted only for two exact disposable contexts, with both exact sentinel
values: `APP_ENV=production` at `https://gradex.localhost:18443` for local
production-like acceptance, and `APP_ENV=staging` at
`https://staging.gradex.network` for public LG-019 acceptance. These paired
contexts are not a general production bypass. The public LG-019 staging context
does not represent a registered commercial entity and is not commercial-launch
legal evidence.

`identity.PolicySetResolver` remains the single admission policy boundary. A
distinct production resolver exposes the approved ID, version, effective date,
localized labels, and canonical URLs derived from `PUBLIC_ORIGIN`. The existing
static resolver remains a development/test fixture. Production composition
selects the production resolver and the already-approved HIBP source; missing,
stale, or inconsistent configuration fails closed.

The existing append-only `policy_acceptances` records remain authoritative:
each registration persists the exact policy-set ID, locale, and the approved
Privacy and Terms versions. A future resolver version cannot rewrite those
historical rows. No schema change is required.

Student admission and staff invitation admission remain separate composition
surfaces. The production Student registration path must not be blocked by the
already-unavailable staff-invitation foundation. Production staff invitation
routes therefore remain unmounted and fail closed; this remediation does not
approve or expand staff behavior.

## Frontend ownership

The four App Router pages render the Product Owner text without material
rewriting:

- `/ar/privacy`
- `/en/privacy`
- `/ar/terms`
- `/en/terms`

The authoritative Markdown bodies are mechanically extracted into a checked-in
generated TypeScript module because the frontend container build context does
not contain repository-level documentation. The frontend test command checks
that each generated policy body is
byte-for-byte current with its corresponding section in the authoritative
package. A small trusted Markdown renderer handles only headings, paragraphs,
bold spans, and explicit line breaks used by the approved source.

Server-side legal configuration interpolates the registration number and
registered address and displays all configured operator/contact values. Legal
route metadata derives canonical and language-alternate URLs from
`PUBLIC_ORIGIN`. The pages are public, indexable, responsive, accessible, and
provide language navigation.

The registration form continues to fetch the current set from the Go API,
leaves every consent unchecked, links to the resolver URLs, and makes the exact
policy-set version/effective date visible.

## Validation and evidence

Tests cover production resolver metadata and URL derivation, stale/invalid
sets, configuration validation, exact sentinel scoping, append-only acceptance
evidence, legal-source parity, route content/accessibility, and production
composition with HIBP. The full S11 browser journey must begin with the shipped
registration form and run locally and against the disposable HTTPS topology.
S11 evidence records the remediation but does not self-approve independent
closure. Actual public legal registration number and registered address remain
external prerequisites for T047.
