package email

import (
	"testing"
	"time"
)

func TestFakeSenderDeduplicatesByStableKey(t *testing.T) {
	sender := NewFakeSender()
	message := validTestMessage()
	first, err := sender.Send(t.Context(), message, "gradex/event")
	if err != nil {
		t.Fatal(err)
	}
	second, err := sender.Send(t.Context(), message, "gradex/event")
	if err != nil {
		t.Fatal(err)
	}
	if first != second || len(sender.Messages()) != 1 {
		t.Fatalf("first=%+v second=%+v captured=%d", first, second, len(sender.Messages()))
	}
}

func TestRetryDelayUsesTheFixedBoundedSchedule(t *testing.T) {
	cases := []struct {
		attempt int
		want    time.Duration
	}{{1, 30 * time.Second}, {2, 2 * time.Minute}, {3, 10 * time.Minute}, {4, 30 * time.Minute}, {5, 30 * time.Minute}}
	for _, testCase := range cases {
		if got := retryDelay(testCase.attempt); got != testCase.want {
			t.Errorf("attempt %d delay = %s, want %s", testCase.attempt, got, testCase.want)
		}
	}
}
