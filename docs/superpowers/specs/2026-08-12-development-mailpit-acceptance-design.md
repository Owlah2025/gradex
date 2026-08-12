# Development-only Mailpit acceptance design

**Date:** 2026-08-12  
**Scope:** local founder acceptance only

## Goal

Deliver real transactional action emails to an ephemeral local browser mailbox,
without adding a Gradex token-reading API or changing any production email
behavior.

## Options considered

1. **Mailpit behind the existing `email.Sender` interface — selected.** The
   worker continues to claim protected-outbox records and render the existing
   links; the new adapter sends that already-rendered message to local SMTP.
   Mailpit's browser UI displays the result exactly as an email recipient sees
   it.
2. **Expose fake sender captures through a Gradex endpoint — rejected.** It
   would make action tokens available through an application API and bypass the
   delivery boundary the acceptance run is intended to exercise.
3. **Use a live/sandbox Resend account — rejected.** It would require external
   credentials and does not provide the self-contained local acceptance
   environment requested.

## Design

- Add a `mailpit` transactional-email provider at the worker composition
  boundary. Its sender serializes the existing provider-neutral `email.Message`
  to unauthenticated local SMTP.
- Add `EMAIL_SMTP_ADDR` as the Mailpit provider's non-secret loopback address.
  The local acceptance configuration uses `127.0.0.1:1025`.
- Permit `EMAIL_PROVIDER=mailpit` only with `APP_ENV=development`; reject it in
  staging and production during configuration loading. Production continues to
  require `EMAIL_PROVIDER=resend` and an `EMAIL_API_KEY` unchanged.
- Add a `mailpit` service to the existing development Compose file, exposing
  SMTP at `127.0.0.1:1025` and its UI at `http://127.0.0.1:8025`. Do not add a
  volume: ordinary API/worker/frontend restarts retain messages, while Compose
  stack removal clears them.
- The browser mailbox, rather than any Gradex API or database query, is the
  sole local mechanism for consuming Instructor invitation, verification,
  reset, and Course Access Invitation links.

## Verification

- Unit-test Mailpit message serialization against a local SMTP listener.
- Extend configuration tests to prove development permits the provider and
  staging/production reject it; preserve the existing Resend production case.
- Start the founder acceptance stack, trigger actual outbox messages, inspect
  the Mailpit UI, and open the rendered links in the browser flow.

## Non-goals

- No production Compose, deployment, Resend, frontend, or Gradex API changes.
- No persistent mailbox storage, new credentials, SQL access, or test-only
  authorization shortcuts.
