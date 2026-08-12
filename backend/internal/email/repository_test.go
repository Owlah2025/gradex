package email

import (
	"testing"
	"time"
)

func TestClaimBatchAcceptsMailpitAlongsideExistingProviders(t *testing.T) {
	for _, provider := range []string{"fake", "mailpit", "resend"} {
		t.Run(provider, func(t *testing.T) {
			if err := (claimBatch{provider: provider, now: time.Now(), lease: time.Minute, limit: 1}).validate(); err != nil {
				t.Fatalf("provider %q rejected: %v", provider, err)
			}
		})
	}
}
