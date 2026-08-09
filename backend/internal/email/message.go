// Package email delivers Gradex's fixed transactional messages without
// exposing provider concepts to identity, access, or other domain modules.
package email

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
)

const MaxAttempts = 5

// Message is the launch-sized provider-neutral delivery contract.
type Message struct {
	From      string
	Recipient string
	ReplyTo   string
	Subject   string
	Text      string
	HTML      string
}

type SendResult struct {
	ProviderMessageID string
}

type Sender interface {
	Provider() string
	Send(context.Context, Message, string) (SendResult, error)
}

type FailureKind string

const (
	FailureTransient FailureKind = "transient"
	FailurePermanent FailureKind = "permanent"
)

// SendFailure contains only bounded safe classifications. Raw provider error
// text and response bodies never cross the adapter boundary.
type SendFailure struct {
	Kind       FailureKind
	Class      string
	Code       string
	RetryAfter time.Duration
}

func (e *SendFailure) Error() string {
	if e == nil {
		return "transactional email send failed"
	}
	return fmt.Sprintf("transactional email send failed (%s/%s)", e.Kind, e.Class)
}

func (e *SendFailure) Transient() bool { return e != nil && e.Kind == FailureTransient }

func AsSendFailure(err error) (*SendFailure, bool) {
	var failure *SendFailure
	return failure, errors.As(err, &failure)
}

func validateMessage(message Message) error {
	for name, value := range map[string]string{"from": message.From, "recipient": message.Recipient, "subject": message.Subject} {
		if err := validateMessageHeader(name, value); err != nil {
			return err
		}
	}
	if _, err := mail.ParseAddress(message.From); err != nil {
		return errors.New("transactional message sender is invalid")
	}
	if !validBareMailbox(message.Recipient) {
		return errors.New("transactional message recipient is invalid")
	}
	if message.ReplyTo != "" && !validBareMailbox(message.ReplyTo) {
		return errors.New("transactional message reply-to is invalid")
	}
	if strings.TrimSpace(message.Text) == "" || strings.TrimSpace(message.HTML) == "" || strings.ContainsRune(message.Text+message.HTML, '\x00') {
		return errors.New("transactional message content is invalid")
	}
	return nil
}

func validateMessageHeader(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("transactional message %s is required", name)
	}
	if strings.ContainsAny(value, "\r\n\x00") {
		return fmt.Errorf("transactional message %s is invalid", name)
	}
	return nil
}

func validBareMailbox(value string) bool {
	mailbox, err := mail.ParseAddress(value)
	return err == nil && mailbox.Address == value && !strings.ContainsAny(value, "\r\n\x00")
}

func retryDelay(attempt int) time.Duration {
	switch attempt {
	case 1:
		return 30 * time.Second
	case 2:
		return 2 * time.Minute
	case 3:
		return 10 * time.Minute
	default:
		return 30 * time.Minute
	}
}
