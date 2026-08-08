#!/usr/bin/env bash

set -euo pipefail

S11_SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
S11_ROOT="$(cd "$S11_SCRIPT_DIR/../.." && pwd)"

export GRADEX_STAGING_SMOKE_MODE=s11

(cd "$S11_ROOT/backend" && go test -tags=integration ./internal/httpapi ./internal/identity \
  -run 'Test(CompleteStudentAuthenticationJourney|BatchA_CourseAccessInvitationHTTPAPI_RealPostgreSQL|BatchB_GrantConcurrency_RealPostgreSQL|DenialsAreByteIdentical|EveryProtectedLearningRouteRevalidates|EveryDenialLeavesEntitlementEnrollmentAndProgressUnchanged|VerificationResendSupersedesAndConsumptionIsSingleUse|ResetSecretRefusesExpiredReplayedAndWrongPurpose)$' \
  -count=1)

exec "$S11_SCRIPT_DIR/verify-staging-smoke.sh" "$@"
