package email

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type CapturedMessage struct {
	Message        Message
	IdempotencyKey string
	ProviderID     string
}

// FakeSender is deterministic and intended only for development and tests.
type FakeSender struct {
	mu       sync.Mutex
	messages []CapturedMessage
	nextErr  error
}

func NewFakeSender() *FakeSender { return &FakeSender{} }

func (*FakeSender) Provider() string { return "fake" }

func (s *FakeSender) Send(ctx context.Context, message Message, idempotencyKey string) (SendResult, error) {
	if err := ctx.Err(); err != nil {
		return SendResult{}, err
	}
	if err := validateMessage(message); err != nil {
		return SendResult{}, &SendFailure{Kind: FailurePermanent, Class: "invalid_message", Code: "invalid_message"}
	}
	if idempotencyKey == "" {
		return SendResult{}, errors.New("transactional email idempotency key is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.nextErr != nil {
		err := s.nextErr
		s.nextErr = nil
		return SendResult{}, err
	}
	for _, captured := range s.messages {
		if captured.IdempotencyKey == idempotencyKey {
			return SendResult{ProviderMessageID: captured.ProviderID}, nil
		}
	}
	providerID := fmt.Sprintf("fake-%04d", len(s.messages)+1)
	s.messages = append(s.messages, CapturedMessage{
		Message: message, IdempotencyKey: idempotencyKey, ProviderID: providerID,
	})
	return SendResult{ProviderMessageID: providerID}, nil
}

func (s *FakeSender) FailNext(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextErr = err
}

func (s *FakeSender) Messages() []CapturedMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]CapturedMessage, len(s.messages))
	copy(result, s.messages)
	return result
}
