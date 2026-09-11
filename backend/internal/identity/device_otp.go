package identity

import (
	"io"
	"time"
)

// The device-trust challenge reuses the Student email OTP implementation whole:
// the same peppered HMAC, the same six digits, the same attempt budget, the
// same resend cooldown, the same action-secret row shape, and the same
// encrypted outbox delivery. Only the purpose domain differs.
//
// That reuse is deliberate. A second OTP implementation would restate every one
// of those compensating controls — the short TTL, the single live challenge,
// the bounded guessing budget — and each restatement is a place for them to
// drift apart. What must *not* be shared is validity: a code mailed to trust a
// device may never verify an email address, and vice versa, which is exactly
// what the separate domain constant buys.
const deviceTrustOTPDomain = "gradex-student-device-trust-otp-v1"

// deviceTrustTemplateContract is its own message, not a variant of the
// verification template, for the reason 0029 already recorded about contracts:
// the dispatcher selects rendering by contract, and one contract that can
// render two different meanings is one branch away from mailing the wrong one.
const deviceTrustTemplateContract = "student-device-trust-otp-v1"

type deviceTrustOTPOptions struct {
	Pepper EmailOTPPepper
	Now    time.Time
	TTL    time.Duration
	Random io.Reader
}

func newDeviceTrustOTP(options deviceTrustOTPOptions) (IssuedEmailOTP, error) {
	return newEmailOTP(emailOTPOptions{
		Pepper: options.Pepper,
		Now:    options.Now,
		TTL:    options.TTL,
		Random: options.Random,
		Domain: deviceTrustOTPDomain,
	})
}

// MatchesDeviceTrustOTP compares a presented code against a stored device-trust
// digest in constant time. A code minted for any other purpose cannot match.
func (p EmailOTPPepper) MatchesDeviceTrustOTP(challengeID, code string, storedDigest []byte) bool {
	return p.matches(deviceTrustOTPDomain, challengeID, code, storedDigest)
}
