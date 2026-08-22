# ST-19 Automated Manual Course Purchase Flow — remediation evidence

**Recorded:** 2026-08-21T06:14:48+03:00

This record closes the ST-19 remediation findings F-1 through F-9. It is
evidence for the functional-completion tracker, not a replacement for the
canonical business rules or founder decision D-090.

## Focused gates

```text
bash deploy/scripts/verify-compose-render.sh
  PASS: production-like and Hostinger renders receive SALES_WHATSAPP_NUMBER;
  production-like render rejects a missing value.

cd backend && go test -tags=integration ./internal/httpapi -run
  'Test(ManualPurchaseFlowHTTPAPI|PurchaseInvitationCancellation|
  AdminPurchaseRequestCancellation|PurchasePaymentConfirmationMaps)' -count=1
  PASS (4.268s)

cd backend && go test -tags=integration ./internal/db -run
  'Test(MigrateUpDownUp|ManualPurchaseRollbackGuard)' -count=1
  PASS (3.598s)
cd backend && go test -tags=integration ./cmd/migrate -run
  TestDownRefusesLivePurchaseEntitlementBeforeMigrationStateChanges -count=1
  PASS (0.900s): the actual canonical down-command preflight refused a live
  PURCHASE_REQUEST entitlement before the migration marker or schema changed.

cd backend && go test ./internal/ratelimit ./internal/config -run
  'TestPurchaseRequestsPolicyBindsNormalizedEmailAndSourceAddress|Test.*SalesWhatsApp' -count=1
  PASS

cd backend && go build ./... && go vet ./... && go test ./...
  PASS
cd backend && go test -p 1 -tags=integration ./...
  PASS
cd frontend && npm test && npm run typecheck
  PASS: 291 tests passed, 0 failed
```

Browser-focused proof used the canonical single worker:

```text
npx playwright test e2e/manual-purchase-flow.spec.ts --workers=1 --reporter=line
  3 passed (1.1m)
npx playwright test e2e/s6-course-access-grant-launch.spec.ts --workers=1 --reporter=line
  2 passed (1.1m)
```

The first browser proof covers the new Student primary journey, existing
Student path, and Admin cancellation/recovery. The second retains the standard
Invitation lifecycle: acceptance leaves it `PENDING_ADMIN_APPROVAL` with no
Entitlement until the separate Admin approval.

## Final canonical Playwright reporter

Host load was checked before the run; no media-authoring worker was running.
The exact command was:

```text
cd frontend && npx playwright test --workers=1 --reporter=line
```

The final textual reporter output was:

```text
6 failed
  [chromium] › e2e/s5-expired-entitlement.spec.ts:712:7
  [chromium] › e2e/s5-playback-performance.spec.ts:157:11
  [chromium] › e2e/s5-viewport-evidence.spec.ts:223:11  (phone)
  [chromium] › e2e/s5-viewport-evidence.spec.ts:223:11  (tablet)
  [chromium] › e2e/s5-viewport-evidence.spec.ts:223:11  (laptop)
  [chromium] › e2e/s5-viewport-evidence.spec.ts:223:11  (desktop)
3 did not run
114 passed (7.9m)
```

The six failure identities exactly match the retained baseline. The additional
pass relative to 113/6/3 is ST-19's cancellation/recovery browser test; all
three `manual-purchase-flow` tests and both `s6-course-access-grant-launch`
tests were green in this same run.
