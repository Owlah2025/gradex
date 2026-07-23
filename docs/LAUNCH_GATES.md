# Gradex Launch Gates

> Status: Active
> Last Updated: 2026-07-23

This register separates unresolved production-readiness work from approved product scope. An open
gate does not silently become a requirement or assumed answer. It blocks the named milestone and
production release until its exit evidence exists.

Status values: `OPEN`, `RESOLVED`, `DEFERRED`.

The consolidated placeholder messages and send-tracking table are in the
[2026-07-24 launch-gate outreach pack](launch/outreach/2026-07-24-launch-gate-outreach.md).
They remain drafts until every placeholder is replaced and the message actually leaves the
sender's account.

## Required for MVP

| ID | Gate | Owner | Next action / due | Exit evidence | Blocking point | Status |
|---|---|---|---|---|---|---|
| LG-001 | Choose Instructor revenue-share percentage | Founder + finance/accounting | Founder sends the global-percentage/effective-date decision brief to accounting by July 24 | Approved numeric global percentage and effective date; configured with no code default | Payout configuration and production | OPEN |
| LG-002 | Approve full/partial refund eligibility | Founder + Kuwaiti counsel | Founder sends the proposed eligibility, request, exception, timing, and entitlement questions to counsel by July 24 | Bilingual policy defining eligibility, request process, exceptions, timing, entitlement effect, and version | Refund-policy rules/acceptance sign-off and production | OPEN |
| LG-003 | Approve data-retention schedule | Counsel + accounting + engineering | Engineering prepares the data-class inventory for counsel/accounting review by July 26 | Per-data-class retention/deletion/anonymization schedule for identity, learning, commerce, security, media, and audit | Retention/deletion job design sign-off and production | OPEN |
| LG-004 | Confirm privacy-regulation applicability and data-subject process | Kuwaiti counsel | Founder sends the data-flow summary and applicability/rights questions to counsel by July 24 | Written applicability analysis, controller/provider obligations, cross-border treatment, rights workflow, and notice wording | Privacy/data-flow design sign-off and production | OPEN |
| LG-005 | Confirm Digital Commerce Law operative date/registration | Founder + Kuwaiti counsel/MOCI | Founder requests Gazette/implementing-regulation and registration guidance by July 24 | Gazette/implementing-regulation evidence and completed required registration/disclosures | Public commerce launch | OPEN |
| LG-006 | Confirm education-sector licensing position | Founder + Kuwaiti counsel/authority | Founder requests written licensing/non-applicability guidance by July 24 | Written confirmation of required license/registration or documented non-applicability | Public Course launch | OPEN |
| LG-007 | Complete commercial/payment prerequisites | Founder + finance | Founder inventories commercial-registration, bank, and Tap onboarding gaps by July 24 | Active commercial registration/business-bank requirements and Tap production merchant account | Production payment activation | OPEN |
| LG-008 | Confirm Tap approval for digital Courses and MVP methods | Founder + engineering + Tap | Founder asks Tap to confirm the merchant category plus card/KNET production sources by July 24 | Written production approval for merchant category and enabled card/KNET sources | Final checkout configuration and production | OPEN |
| LG-009 | Verify Tap refund capability per enabled method | Engineering + Tap | Engineering requests method-specific full/partial refund and asynchronous-status sandbox evidence by July 26 | Sandbox/contract evidence for full/partial refunds, asynchronous status, reconciliation, and unsupported behavior | Refund adapter acceptance and production | OPEN |
| LG-010 | Verify Tap webhook authenticity contract | Engineering + Tap | Engineering requests the signed-webhook procedure, replay rules, and test vectors by July 26 | Official verification procedure, replay handling, test vectors, and successful end-to-end verification | Payment/refund adapter acceptance and production | OPEN |
| LG-011 | Approve bilingual customer policies | Founder + counsel | Founder commissions Arabic/English Privacy, Terms, Refund, and checkout drafts after LG-002/004 guidance, no later than July 28 | Published Arabic/English Privacy Notice, Terms, Refund Policy, and checkout disclosures with version identifiers | Public registration/checkout production | OPEN |
| LG-012 | Set launch Course and Section catalog prices | Founder + Admin operations | Founder prepares the launch catalog and price sheet by August 3 | Approved prices entered through audited Admin process | Catalog purchase activation | OPEN |
| LG-013 | Assign community/support ownership | Founder + operations | Founder names the support/community owner and drafts moderation/escalation expectations by July 30 | Named owner, moderation/escalation rules, response expectation, support route, and active community links | Student support/community launch | OPEN |
| LG-014 | Select and validate upload malware scanning | Engineering | Engineering shortlists a scanner against file types, size limits, failure mode, and hosting constraints by July 27 | Selected scanner/service, fail-closed quarantine workflow, supported file limits, alerting, and validation evidence | Downloadable/public asset pipeline acceptance | OPEN |
| LG-015 | Validate accessibility boundary and public claims | Product + engineering | Product schedules the platform UI/player audit and hosted-checkout assessment by August 7 | WCAG 2.2 AA audit for platform-owned UI/player controls, hosted-checkout assessment, and claim copy disclosing the caption gap | Public release and accessibility claims | OPEN |
| LG-016 | Confirm tax, invoicing, receipt, and accounting treatment | Founder + accounting + counsel | Founder sends the commerce-data/accounting questions to accounting and counsel by July 24 | Written treatment for taxes/fees, required invoice/receipt fields and numbering, refund documents, currency rounding, and record retention | Order/receipt/statement data-contract sign-off and production | OPEN |
| LG-017 | Approve payment dispute/chargeback operations | Founder + accounting + Tap + counsel | Founder requests Tap/accounting/counsel inputs for the dispute lifecycle by July 26 | Documented detection/reconciliation, Student/Entitlement policy, evidence handling, revenue/payout adjustment, notifications, and audit process | Final commerce/payout state design and production | OPEN |
| LG-018 | Select and validate transactional email delivery | Engineering + operations | Engineering compares providers/data-processing boundaries and confirms sender-domain access by July 26 | Approved provider/data-processing boundary, production sender domain with SPF/DKIM/DMARC, verified templates/links, bounce/suppression handling, rate limits, monitoring, and deliverability test evidence | Auth/notification adapter acceptance and production | OPEN |
| LG-019 | Approve production operating and recovery envelope | Founder + engineering | Founder supplies budget/load/availability constraints for the July 25 architecture session | System design/deployment record covering expected launch load/storage/egress, budget, availability, RPO/RTO, managed secrets, monitoring/alerting, incident runbooks, restore test, security review, and representative load test | Production architecture sign-off and release | OPEN |
| LG-020 | Approve Instructor agreement and content-rights process | Founder + Kuwaiti counsel + operations | Founder sends the agreement/content-rights brief to counsel and operations by July 24 | Signed bilingual/appropriate agreement covering content ownership/license/permissions, revenue share/effective version, payout/tax treatment, warranties, takedown/moderation, termination, and Course asset handoff; launch Courses have evidence | Instructor onboarding and public Course launch | OPEN |

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
