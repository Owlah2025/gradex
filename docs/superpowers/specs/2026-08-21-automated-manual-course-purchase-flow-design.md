# Automated Manual Course Purchase Flow — Approved Design

**Date:** 2026-08-21  
**Status:** Founder-approved implementation design  
**Authority:** The founder decision recorded in `docs/DECISIONS.md` as D-090 is canonical. This document is an implementation design, not a competing authority.

## Protected baseline and feature inventory

The implementation starts on a protected, user-owned dirty worktree: **105 modified tracked paths and 54 untracked paths**. It is never reset, stashed, restored, cleaned, or normalised by this work. The feature inventory begins with this document and subsequent explicitly purchase-named migrations, packages, tests, E2E, and documentation additions. Before an existing shared file is edited, its working-tree diff is inspected and only an additive, purchase-required change is made.

## Contract

`Published Course Details → Email → persisted Purchase Request → browser WhatsApp handoff → Admin purchase queue → Confirm Payment & Send Invitation → queued invitation email → registration/verification/login when needed → same invitation context → accept → active entitlement → Course Home`.

The two invitation modes deliberately coexist:

- Standard manual invitation: `PENDING_STUDENT_ACCEPTANCE → PENDING_ADMIN_APPROVAL → APPROVED`; the existing Admin approval is the sole entitlement grant.
- Purchase-backed invitation: payment confirmation creates a pre-authorized invitation. A matching Student's valid acceptance atomically grants access; there is no second Admin approval.

The browser never supplies a price, payment state, preauthorization, course relationship, entitlement state, or invitation relationship as authority.

## Domain and persistence

Introduce a small `purchase_requests` aggregate with a public safe reference, Course and normalized email, optional authenticated Student, integer KWD fils snapshot, factual state, timestamped transitions, and an invitation link. The database enforces one active request per `(course_id, normalized_email)`, one request-to-invitation relationship, and valid state/link combinations. The server reads the currently public Course and snapshots its authoritative integer `price_minor_units`; the client may identify only the Course.

The existing Course Access Invitation remains the canonical invitation implementation. It gains a server-authoritative purchase relationship / preauthorization representation, not a browser-controlled flag. Acceptance detects that relationship inside the existing locked transaction and either preserves the legacy pending-Admin path or creates/reuses exactly one enrollment and active entitlement in the same transaction. Entitlement provenance remains explicit and auditable.

## Commands and UI

Public `POST` creation accepts only Course identity and email, validates public availability server-side, normalizes email, deduplicates active requests, and returns the persisted request's safe reference plus a server-composed WhatsApp URL. No account/existing-access information is exposed.

Admin reads a searchable title/email/reference/status queue and calls one semantic confirm command. That command locks the request, revalidates course eligibility, records external payment confirmation, creates/reuses exactly one invitation and invitation-email outbox event, links the aggregate, and commits atomically. Repeats return the same authoritative result.

Course Details adds a localized, email-first action without redesigning the surface. Successful submission uses direct browser navigation to the server-returned WhatsApp URL. Failure does not navigate. Admin receives a minimal queue in the existing Admin shell.

## Authentication and secrecy

Existing fragment-token capture, scrub, and release remains the sole invitation-token mechanism. Registration, verification, and login carry only validated internal `returnTo`; they must return the Student to the same invitation context without moving the secret into an ordinary query parameter. The existing identity-match refusal stays server-authoritative.

## Verification

The retained primary browser journey uses an unregistered runtime email and proves persistence before intercepted WhatsApp navigation, real Admin confirmation, one outbox invitation, registration/verification/login handoff, automatic grant, and Course Home/protected learning. Focused tests cover existing-account, wrong identity, price snapshot, duplicate public request, duplicate confirmation, authorization, non-public Course refusal, and the legacy manual-invitation lifecycle. The full canonical Playwright suite is compared only to the protected baseline: `111 passed / 6 failed / 3 did not run`, with the six named failures unchanged by identity.
