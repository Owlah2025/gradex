# Gradex Documentation

> Status: Platform architecture approved; detailed system design and production launch gates remain open
> Last updated: 2026-07-25

Gradex is a responsive Arabic/English web learning platform for university Students. The MVP lets
Students discover and purchase Course or Section access, learn through protected video and
Resources, complete available Labs, join Course-scoped external-link office hours, and receive
support. Instructors own assigned Course content but never pricing. Admins control commercial and
operational workflows.

## Start Here

Read these documents in order before system design:

1. [PROJECT_VISION.md](PROJECT_VISION.md) — users, problem, value, principles, and scope.
2. [PRD.md](PRD.md) — authoritative MVP, fast-follow, and out-of-scope requirements.
3. [BUSINESS_RULES.md](BUSINESS_RULES.md) — testable product and policy rules.
4. [DECISIONS.md](DECISIONS.md) — approved decisions and their rationale.
5. [DOMAIN_MODEL.md](DOMAIN_MODEL.md) — conceptual entities, ownership, relationships, and states.
6. [LAUNCH_GATES.md](LAUNCH_GATES.md) — unresolved operational, legal, and vendor decisions that
   block production launch but do not block system design where noted.

## Experience Definition

| Document | Purpose |
|---|---|
| [USER_JOURNEYS.md](USER_JOURNEYS.md) | End-to-end Student, Instructor, and Admin outcomes |
| [SCREENS.md](SCREENS.md) | Canonical MVP screen inventory and per-screen contracts |
| [NAVIGATION_MAP.md](NAVIGATION_MAP.md) | Public and per-role route hierarchy |
| [NAVIGATION_RULES.md](NAVIGATION_RULES.md) | Shell, route guards, responsive behavior, and state handling |
| [WIREFRAMES.md](WIREFRAMES.md) | Low-fidelity layouts tied to canonical screen IDs |
| [design-system/README.md](design-system/README.md) | Repository-backed visual, responsive, RTL, and accessibility guidance |
| [design/landing-page/LANDING_SPEC.md](design/landing-page/LANDING_SPEC.md) | MVP landing-page contract and current implementation drift |

## Engineering and Feature Artifacts

| Document/location | Purpose |
|---|---|
| [CODING_STANDARDS.md](CODING_STANDARDS.md) | Verified backend/frontend engineering conventions |
| [GLOSSARY.md](GLOSSARY.md) | Canonical product and domain language |
| [launch/PLAN.md](launch/PLAN.md) | Readiness-gated July 23–August 15 delivery schedule and daily operating protocol |
| [launch/STATUS.md](launch/STATUS.md) | Live launch progress, carryover, blockers, gate counts, and forecast |
| [`superpowers/specs/`](superpowers/specs/) | Technical design records; each must defer to current canonical product docs |
| [`../specs/`](../specs/) | Feature specifications, plans, tasks, contracts, and checklists |
| [`../frontend/README.md`](../frontend/README.md) | Current landing/frontend implementation notes |

## Source-of-Truth Order

When documents disagree, use this order until the conflict is reconciled:

1. ratified [project constitution](../.specify/memory/constitution.md);
2. PRD and approved Decisions;
3. Business Rules and Domain Model;
4. journeys, screens, navigation, and wireframes;
5. feature specifications and technical designs;
6. implementation notes and prototypes.

Do not silently choose a lower-level artifact when it conflicts with a higher-level one. Record a
decision or update the affected artifacts together.

## Repository Map

```text
gradex/
├── .specify/       # project constitution, templates, and specification workflow
├── assets/         # repository assets
├── backend/        # Go services, migrations, local dependencies, and backend tests
├── docs/           # product, experience, design, and technical documentation
├── frontend/       # Next.js/TypeScript web frontend
├── infrastructure/ # infrastructure definitions as they are added
├── scripts/        # repository automation
└── specs/          # feature-level specifications and delivery artifacts
```

## Current State

- The reconciled product definition and
  [platform architecture](superpowers/specs/2026-07-25-platform-architecture-design.md) are approved
  system-design inputs; independent architecture review is pending.
- A landing-page frontend and a video/backend technical slice already exist; they are implementation
  evidence, not proof that the full MVP is built.
- Domain/data/state and API/security/integration design remain scheduled before the implementation
  foundation. Affected design decisions must revisit any gate whose resolution point is "before
  affected design sign-off."
- Production launch additionally requires every MVP gate in [LAUNCH_GATES.md](LAUNCH_GATES.md) to
  be resolved and evidenced.

## License

Private repository.
