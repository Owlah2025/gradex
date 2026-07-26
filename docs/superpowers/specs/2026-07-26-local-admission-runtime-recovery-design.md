# Local Student Admission Runtime Recovery Design

**Date:** 2026-07-26  
**Scope:** Local development only

## Problem

The registration page loads, but its first API dependency fails. The running Go API returns
`404 NOT_FOUND` for both:

- `GET /api/v1/session/bootstrap`
- `GET /api/v1/registration-policy-set`

Repository inspection and live requests show that the backend started with Student registration
disabled. Admission routes are intentionally mounted only when their complete fail-closed
configuration is enabled, so the frontend reports that the current terms could not be loaded.

The ignored local `backend/.env` also contains retired minute-based expiry settings. Those settings
make ordinary Make targets fail configuration validation unless they are explicitly overridden.

## Decision

Repair only the developer's ignored `backend/.env` and restart the local API. Do not weaken
fail-closed route composition, add committed development secrets, or change production behavior.

The local configuration will:

1. remove the retired `UPLOAD_URL_EXPIRY_MINUTES` and `PLAYBACK_URL_EXPIRY_MINUTES` settings;
2. retain the duration-based `UPLOAD_URL_EXPIRY` and `PLAYBACK_URL_EXPIRY` settings;
3. set `PUBLIC_ORIGIN=http://localhost:3000`;
4. enable `STUDENT_REGISTRATION_ENABLED`;
5. use the development-only deterministic compromised-password screen;
6. select the existing development registration policy set and protected-payload key version;
7. generate four independent local-only cryptographic values for anonymous-cookie signing, CSRF
   derivation, limiter keying, and protected outbox payloads.

The generated values remain only in the ignored local environment file and must never enter output,
documentation, Git, browser storage, or logs.

## Runtime Flow

After configuration repair:

1. stop only the currently running local API process;
2. start the API through the repository Make target;
3. keep the existing Next development server on port 3000;
4. request anonymous bootstrap through `http://localhost:3000/api/v1`;
5. reuse its host-only cookie to request the localized policy set;
6. load `/register` and confirm the policy checkboxes replace the unavailable-terms message;
7. submit one synthetic local registration and confirm the generic accepted response.

## Failure Handling

- If configuration validation fails, keep the API stopped, report the exact setting name, and do
  not weaken validation.
- If the API starts but admission routes remain absent, inspect the effective non-secret capability
  flags without printing key material.
- If bootstrap or policy retrieval fails through Next but succeeds directly against Go, diagnose
  the development rewrite rather than changing the API contract.
- If registration reaches an unavailable downstream dependency, retain the generic public response
  and diagnose through safe local logs.

## Verification

Recovery is complete only when:

- migration version reports schema 5 and `dirty=false`;
- direct Go liveness succeeds;
- same-origin bootstrap returns `200`;
- same-origin Arabic and English policy reads return `200` with the configured policy-set ID;
- `/register` renders without the unavailable-terms state;
- one synthetic registration returns the fixed generic `202` response;
- no plaintext password, CSRF value, cookie, bearer, or generated local key is printed or committed;
- Git still shows only the pre-existing user-owned untracked files.

