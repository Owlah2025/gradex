# Gradex Launch Gates

> Status: Active
> Last Updated: 2026-07-30

This register separates unresolved production-readiness work from approved product scope. An open
gate does not silently become a requirement or assumed answer. It blocks the named milestone and
production release until its exit evidence exists.

Status values: `OPEN`, `RESOLVED`, `DEFERRED`.

The consolidated placeholder messages and send-tracking table are in the
[2026-08-06 launch-gate outreach pack](launch/outreach/2026-08-06-launch-gate-outreach.md).
The founder explicitly deferred outreach to the final delivery week on 2026-07-23. The gates remain
`OPEN`, the messages remain drafts until actually sent, and the compressed response window is an
accepted launch-schedule risk rather than resolution evidence.

## Required for MVP

| ID | Gate | Owner | Next action / due | Exit evidence | Blocking point | Status |
|---|---|---|---|---|---|---|
| LG-001 | Choose Instructor revenue-share percentage | Founder + finance/accounting | Founder sends the global-percentage/effective-date decision brief on August 6 | Approved numeric global percentage and effective date; configured with no code default | Payout configuration and production | OPEN |
| LG-002 | Approve full/partial refund eligibility | Founder + Kuwaiti counsel | Founder sends the refund-policy questions on August 6 | Bilingual policy defining eligibility, request process, exceptions, timing, entitlement effect, and version | Refund-policy rules/acceptance sign-off and production | OPEN |
| LG-003 | Approve data-retention schedule | Counsel + accounting + engineering | Engineering prepares the data-class inventory for August 6 review | Per-data-class retention/deletion/anonymization schedule for identity, learning, commerce, security, media, and audit | Retention/deletion job design sign-off and production | OPEN |
| LG-004 | Confirm privacy-regulation applicability and data-subject process | Kuwaiti counsel | Founder sends the data-flow summary and privacy questions on August 6 | Written applicability analysis, controller/provider obligations, cross-border treatment, rights workflow, and notice wording | Privacy/data-flow design sign-off and production | OPEN |
| LG-005 | Confirm Digital Commerce Law operative date/registration | Founder + Kuwaiti counsel/MOCI | Founder requests Gazette/implementing-regulation and registration guidance on August 6 | Gazette/implementing-regulation evidence and completed required registration/disclosures | Public commerce launch | OPEN |
| LG-006 | Confirm education-sector licensing position | Founder + Kuwaiti counsel/authority | Founder requests written licensing/non-applicability guidance on August 6 | Written confirmation of required license/registration or documented non-applicability | Public Course launch | OPEN |
| LG-007 | Complete commercial/payment prerequisites | Founder + finance | Founder inventories commercial-registration, bank, and Tap onboarding gaps on August 6 | Active commercial registration/business-bank requirements and Tap production merchant account | Production payment activation | OPEN |
| LG-008 | Confirm Tap approval for digital Courses and MVP methods | Founder + engineering + Tap | Founder asks Tap to confirm the merchant category plus card/KNET production sources on August 6 | Written production approval for merchant category and enabled card/KNET sources | Final checkout configuration and production | OPEN |
| LG-009 | Verify Tap refund capability per enabled method | Engineering + Tap | Engineering requests method-specific refund/sandbox evidence on August 6 | Sandbox/contract evidence for full/partial refunds, asynchronous status, reconciliation, and unsupported behavior | Refund adapter acceptance and production | OPEN |
| LG-010 | Verify Tap webhook authenticity contract | Engineering + Tap | Engineering requests the signed-webhook procedure, replay rules, and test vectors on August 6 | Official verification procedure, replay handling, test vectors, and successful end-to-end verification | Payment/refund adapter acceptance and production | OPEN |
| LG-011 | Approve bilingual customer policies | Founder + counsel | Founder commissions Arabic/English Privacy, Terms, Refund, and checkout drafts on August 6 | Published Arabic/English Privacy Notice, Terms, Refund Policy, and checkout disclosures with version identifiers | Public registration/checkout production | OPEN |
| LG-012 | Set launch Course and Section catalog prices | Founder + Admin operations | Founder prepares the launch catalog and price sheet by August 3 | Approved prices entered through audited Admin process | Catalog purchase activation | OPEN |
| LG-013 | Assign community/support ownership | Founder + operations | Founder names the owner and drafts support/moderation expectations on August 6 | Named owner, moderation/escalation rules, response expectation, support route, and active community links | Student support/community launch | OPEN |
| LG-014 | Select and validate upload malware scanning | Engineering | Engineering shortlists a scanner on August 6 for validation by August 12 | Selected scanner/service, fail-closed quarantine workflow, supported file limits, alerting, and validation evidence | Downloadable/public asset pipeline acceptance | OPEN |
| LG-015 | Validate accessibility boundary and public claims | Product + engineering | Product schedules the UI/player and hosted-checkout assessments on August 6 | WCAG 2.2 AA audit for platform-owned UI/player controls, hosted-checkout assessment, and claim copy disclosing the caption gap | Public release and accessibility claims | OPEN |
| LG-016 | Confirm tax, invoicing, receipt, and accounting treatment | Founder + accounting + counsel | Founder sends the commerce-data/accounting questions on August 6 | Written treatment for taxes/fees, required invoice/receipt fields and numbering, refund documents, currency rounding, and record retention | Order/receipt/statement data-contract sign-off and production | OPEN |
| LG-017 | Approve payment dispute/chargeback operations | Founder + accounting + Tap + counsel | Founder requests Tap/accounting/counsel dispute inputs on August 6 | Documented detection/reconciliation, Student/Entitlement policy, evidence handling, revenue/payout adjustment, notifications, and audit process | Final commerce/payout state design and production | OPEN |
| LG-018 | Select and validate transactional email delivery | Engineering + operations | Engineering starts provider/domain validation on August 6 for completion by August 12 | Approved provider/data-processing boundary, production sender domain with SPF/DKIM/DMARC, verified templates/links, bounce/suppression handling, rate limits, monitoring, and deliverability test evidence | Auth/notification adapter acceptance and production | OPEN |
| LG-019 | Approve production operating and recovery envelope | Founder + engineering | Founder supplies budget/load/availability/RPO/RTO inputs on August 6; design remains provisional until then | System design/deployment record covering expected launch load/storage/egress, budget, availability, RPO/RTO, managed secrets, monitoring/alerting, incident runbooks, restore test, security review, and representative load test | Production architecture sign-off and release | OPEN |
| LG-020 | Approve Instructor agreement and content-rights process | Founder + Kuwaiti counsel + operations | Founder sends the agreement/content-rights brief on August 6 | Signed bilingual/appropriate agreement covering content ownership/license/permissions, revenue share/effective version, payout/tax treatment, warranties, takedown/moderation, termination, and Course asset handoff; launch Courses have evidence | Instructor onboarding and public Course launch | OPEN |
| LG-021 | Select and validate compromised-password screening source | Engineering + security | Engineering shortlists a privacy-preserving provider or licensed offline dataset on August 6 for validation by August 12 | Approved source and license; full passwords never leave the credential boundary; privacy-preserving query/storage contract; outage and fail-closed policy; deterministic test vectors; latency/error monitoring; successful staging validation | Credential-creation adapter acceptance and production registration/invitation/recovery/password change | OPEN |

### Effect on System Design

Platform system design can start now. For open items whose blocking point is a design sign-off:

- define an explicit configuration/policy/provider boundary;
- preserve immutable source events and required auditability;
- do not hard-code an undecided percentage, retention period, tax treatment, refund eligibility, or
  chargeback entitlement outcome;
- keep email, storage/CDN, secrets, monitoring, and backup/recovery choices replaceable and
  testable until the production operating envelope is approved;
- return to this register before finalizing the affected subsystem.

The missing numeric revenue share does not prevent the payout formula/data model: use a required
versioned configuration with no default. Legal/accounting/provider gates do prevent representing an
assumption as production policy.

### July 29 Architecture-Boundary Audit

The July 29 deadline in [the launch plan](launch/PLAN.md#7-gate-deadlines) was met for all 14
then-open gates that affected architecture. `LG-021` was added on July 30 when S1A review exposed
the missing compromised-password production source; its provider boundary is recorded in the
[S1B delivery design](superpowers/specs/2026-07-30-s1b-delivery-design.md#41-security-prerequisites).
All 15 current architecture-affecting gates therefore have a documented configurable, policy,
evidence, or provider boundary:

| Gate | Documented boundary while open |
|---|---|
| `LG-001` | Reporting requires a versioned revenue-share configuration with no code default; no earning is calculated without an effective approved row ([domain §16.1](superpowers/specs/2026-07-26-domain-data-state-design.md#161-versioned-share-and-append-only-ledger)). |
| `LG-002` | Refund eligibility and Entitlement effects remain versioned/configurable; unsupported policy refuses the Refund command ([domain §8.3](superpowers/specs/2026-07-26-domain-data-state-design.md#83-refund-state)). |
| `LG-003` | Retention uses versioned per-data-class policy, and destructive jobs stay disabled while the gate is open ([domain §19.1](superpowers/specs/2026-07-26-domain-data-state-design.md#191-configurable-policy-boundary)). |
| `LG-004` | Region placement remains portable and provisional; privacy-control records preserve evidence without inventing applicability, rights, residency, or cross-border conclusions ([platform §3.5](superpowers/specs/2026-07-25-platform-architecture-design.md#35-provisional-region-boundary), [domain §19.1](superpowers/specs/2026-07-26-domain-data-state-design.md#191-configurable-policy-boundary)). |
| `LG-008` | Tap merchant and method selection stays behind the configurable hosted-payment adapter rather than entering domain logic ([platform §14](superpowers/specs/2026-07-25-platform-architecture-design.md#14-open-decisions-and-gates)). |
| `LG-009` | Refund state models asynchronous and unsupported provider behavior, and the command refuses an unapproved provider/method capability ([domain §8.3](superpowers/specs/2026-07-26-domain-data-state-design.md#83-refund-state)). |
| `LG-010` | Tap authenticity is an adapter contract with official vectors; production processing stays disabled until it is tested and approved ([API design §8](superpowers/specs/2026-07-27-api-security-integration-design.md#8-provider-ingress-and-reconciliation)). |
| `LG-014` | The scanner adapter is provider-neutral; missing configuration, outage, ambiguity, or exhaustion leaves the exact Asset Version quarantined and unavailable ([API design §7.1](superpowers/specs/2026-07-27-api-security-integration-design.md#71-malware-scanning-adapter)). |
| `LG-015` | Platform-owned UI/player validation and hosted-checkout/caption limitations form the claim boundary; complete conformance is not claimed while the gap remains ([platform §13](superpowers/specs/2026-07-25-platform-architecture-design.md#13-validation-and-production-acceptance)). |
| `LG-016` | Commerce and Reporting preserve immutable commercial-document and transaction evidence without assuming tax, numbering, invoice, receipt, or statement rules ([platform §14](superpowers/specs/2026-07-25-platform-architecture-design.md#14-open-decisions-and-gates), [domain §16.1](superpowers/specs/2026-07-26-domain-data-state-design.md#161-versioned-share-and-append-only-ledger)). |
| `LG-017` | Disputes remain immutable provider evidence whose Entitlement, revenue, payout, notification, and recovery effects require approved policy ([platform §14](superpowers/specs/2026-07-25-platform-architecture-design.md#14-open-decisions-and-gates)). |
| `LG-018` | Transactional email uses replaceable delivery attempts, provider/sender configuration, suppression, and monitoring; email failure cannot reverse the durable in-app notification or source action ([domain §15.1](superpowers/specs/2026-07-26-domain-data-state-design.md#151-durable-record-and-delivery-evidence)). |
| `LG-019` | Load, cost, region, scaling, availability, backup, RPO, and RTO remain explicitly provisional; the split managed topology keeps services and providers independently replaceable until approval ([platform §§2–3](superpowers/specs/2026-07-25-platform-architecture-design.md#2-architecture-decision)). |
| `LG-020` | Identity and Moderation retain immutable versioned agreement and content-rights evidence without inventing terms ([platform §14](superpowers/specs/2026-07-25-platform-architecture-design.md#14-open-decisions-and-gates)). |
| `LG-021` | Credential creation depends on a provider-neutral compromised-password checker; unavailable or unconfigured screening fails the affected command closed, local tests use a deterministic adapter, and production activation waits for source/license/privacy/availability evidence ([S1B delivery design §4.1](superpowers/specs/2026-07-30-s1b-delivery-design.md#41-security-prerequisites)). |

`LG-005`–`LG-007` and `LG-011`–`LG-013` do not introduce an additional architecture choice:
they resolve regulatory/licensing findings, commercial-account prerequisites, published policy
content, launch catalog data, and named support ownership. Their production blocking points remain
open exactly as listed above. This audit records design containment only; it resolves no launch
gate and is not production-readiness evidence.

## Fast-Follow Gates

These do not block MVP production because their features remain outside MVP.

| ID | Feature | Owner | Exit evidence | Status |
|---|---|---|---|---|
| FF-001 | Deema BNPL | Founder + finance + engineering | Written digital-goods approval, enabled source, reviewed payment/entitlement lifecycle, and updated acceptance tests | DEFERRED |
| FF-002 | Bundles | Product + finance + engineering | Approved pricing, Entitlement duration, refund allocation, Coupon behavior, and Domain Model update | DEFERRED |
| FF-003 | Captions/transcripts | Product + content + engineering | Approved creation/upload workflow, languages/formats, player behavior, and accessibility validation | DEFERRED |
| FF-004 | Instructor earnings dashboard/automated settlement | Product + finance + engineering | Validated Instructor need, provider/contract model, security review, and reconciled accounting design | DEFERRED |
| FF-005 | MFA/social login | Product + security + engineering | Approved threat/identity requirements and provider/recovery design | DEFERRED |
| FF-006 | Lifecycle/marketing notifications | Product + counsel + engineering | Consent/opt-out policy, segmentation/scheduling design, and channel/provider approval | DEFERRED |

## Official Source Baseline

- [Kuwait Government announcement for Digital Commerce Law No. 10 of 2026](https://e.gov.kw/sites/KGOArabic/Pages/ApplicationPages/NewsDetail.aspx?nid=64409149)
- [CITRA Decision No. 26 of 2024 issuing the Data Privacy Protection Regulation](https://www.citra.gov.kw/sites/ar/Pages/DecisionsDetails.aspx?id=6)
- [Tap refund creation API](https://developers.tap.company/reference/create-a-refund)
- [Tap refund response codes](https://developers.tap.company/reference/charge-response-codes)

Official/provider sources must be rechecked when a gate is resolved. This register does not replace
legal advice, accounting advice, provider contracts, or production verification.
