# Production Release Plan — Student UI, Auth, and Transactional Email

**Status:** Prepared locally; requires independent G0a review before production mutation.

This plan supersedes the schema-37/38 Subject Catalogue release framing for this release. It uses
the live production baseline captured read-only on 2026-09-18 and the existing Hostinger tooling in
`deploy/hostinger/release.sh` and `deploy/hostinger/host.sh`. It introduces no migration and no
provider/configuration change.

## Live rollback anchor

| Item | Value |
|---|---|
| Running application revision | `776f02543b105fe428e80627ed52342304dc01f2` |
| Backend / worker image | `gradex-backend:hostinger-776f02543b10` |
| Frontend image | `gradex-frontend:hostinger-776f02543b10` |
| Proof image | `gradex-backend-proof:hostinger-776f02543b10` |
| Backend image ID | `sha256:c3bbcff3aa621db446ac0963d22c5b45faca4541af1d86e4c45dc9c8473f9714` |
| Frontend image ID | `sha256:9026cc088ea03d42cd8c1af736fcd6f787166fb2e639742dc413eb80fc1648a9` |
| Proof image ID | `sha256:f59ea6a843f47cb1451ddbe86466ef59e8ad28cbf4d818fb3f6e1025291c3a8b` |
| Database schema | `38|false` |
| Institutions | `15` |
| Public health/readiness | `/healthz` 200; `/readyz` 200 with Postgres, Redis, schema `ok` |

The prior immutable `4e7ddcdbadda` backend/frontend/proof image tags remain present on the host, but
the running `776f025` images are the primary rollback anchor for this release preparation.

## Release scope

The final release may contain:

- committed Student branding and landing polish already after the live `776f025` baseline;
- the approved Student AuthShell changes;
- the complete 13-contract GradeX transactional email shell and renderer;
- the development-only renderer preview command and email asset, which are not built into the
  production runtime services.

The release contains no migration, pricing change, entitlement behavior change, secret, Resend
configuration change, or production preview route.

## Checkpoints

1. Reconcile the worktree, classify every dirty file, and obtain a clean final HEAD.
2. Run the independent read-only G0a review against the exact baseline-to-final range.
3. Record the fresh Product Owner G0b decision for this exact release.
4. Run frontend/backend/email/production-render gates from the clean final HEAD.
5. Build immutable backend, frontend, and proof images with `deploy/hostinger/release.sh build`.
6. Export and verify the checksum-addressed release with `deploy/hostinger/release.sh export`.
7. Stop for the pre-mutation report showing baseline, release SHA, image IDs, gates, and rollback
   tags.
8. Only after the release gates are satisfied, use the existing `deploy/hostinger/host.sh
   apply-release` workflow. Do not run migrations, reset, reseed, or change the provider.

## Schema and rollback contract

Production is already clean schema 38. This release runs no migration and must leave schema 38
clean. Any production readiness response other than Postgres/Redis/schema `ok` stops the release.
The existing immutable `776f025` images remain available for immediate application rollback.

## Required final evidence

- empty `git status --porcelain`;
- exact baseline-to-release commit range and changed-file audit;
- independent G0a verdict and fresh G0b decision;
- TypeScript, ESLint, frontend tests, clean production build, Go tests, email renderer/preview tests,
  `git diff --check`, clean-code/test guards, secret/exposure guard, and production compose render;
- immutable image tags, OCI revision labels, IDs, and export checksum;
- post-deployment `/healthz`, `/readyz`, schema 38, API, worker, frontend, edge, and Student smoke
  evidence.
