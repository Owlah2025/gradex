// Command gradex-contact-reminder queues the D-118 WhatsApp contact reminder
// email for every ACTIVE Student with a verified email address.
//
// It is an operator command, run inside the API container so it uses that
// container's configuration and outbox key:
//
//	gradex-contact-reminder -campaign whatsapp-contact-2026-10            # dry run: counts only
//	gradex-contact-reminder -campaign whatsapp-contact-2026-10 -send      # queue the emails
//	gradex-contact-reminder -test-to someone@example.com                  # one test email, nothing recorded
//
// The worker delivers the queued messages through the normal transactional
// pipeline. Re-running with the same campaign never emails a Student twice.
// Output is counts only; no address or number is printed.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/mail"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/db"
	"github.com/Owlah2025/gradex/backend/internal/email"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

func main() {
	campaign := flag.String("campaign", "", "campaign name, 3-64 lowercase letters, digits or hyphens (required)")
	send := flag.Bool("send", false, "queue the emails; without it the command only counts")
	testTo := flag.String("test-to", "", "send one test copy to this address only, through the configured provider; writes nothing")
	flag.Parse()
	var err error
	if *testTo != "" {
		err = sendTest(*testTo)
	} else {
		err = run(*campaign, *send)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "gradex-contact-reminder:", err)
		os.Exit(1)
	}
}

func run(campaign string, send bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	pool, err := db.Connect(ctx, cfg.DatabaseURL().Expose())
	if err != nil {
		// Fixed text: a driver error may describe the connection target.
		return errors.New("could not connect to the database")
	}
	defer pool.Close()
	if err := db.CheckSchemaAtLeast(ctx, pool, db.StudentWhatsAppContactSchemaVersion); err != nil {
		return fmt.Errorf("schema check: %w", err)
	}
	var writer *outbox.Writer
	if send {
		admission := cfg.Admission()
		writer, err = outbox.NewWriter(admission.ProtectedPayloadKeyVersion(), []byte(admission.ProtectedPayloadKey().Expose()))
		if err != nil {
			return errors.New("the protected outbox key is not usable")
		}
	}
	result, err := identity.QueueContactReminders(ctx, pool, writer, identity.ContactReminderRequest{
		Campaign: campaign, Send: send, Now: time.Now(),
	})
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}

// sendTest renders the reminder exactly as a Student receives it and hands it
// to the configured provider for one operator-chosen address. It touches no
// table: no outbox row, no delivery row, no audit row, and no Student.
func sendTest(address string) error {
	parsed, err := mail.ParseAddress(strings.TrimSpace(address))
	if err != nil || parsed.Address != strings.TrimSpace(address) {
		return errors.New("test-to must be a plain email address")
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	settings := cfg.Email()
	if !settings.Enabled() {
		return errors.New("transactional email is not enabled in this environment")
	}
	renderer, err := email.NewRenderer(email.RendererOptions{
		PublicOrigin: cfg.PublicOrigin(), FromAddress: settings.FromAddress(),
		FromName: settings.FromName(), ReplyTo: settings.ReplyTo(),
	})
	if err != nil {
		return err
	}
	var sender email.Sender
	switch settings.Provider() {
	case config.EmailProviderFake:
		sender = email.NewFakeSender()
	case config.EmailProviderMailpit:
		sender, err = email.NewMailpitSender(email.MailpitOptions{Address: settings.SMTPAddress(), Timeout: settings.Timeout()})
	case config.EmailProviderResend:
		sender, err = email.NewResendSender(email.ResendOptions{APIKey: settings.APIKey(), Timeout: settings.Timeout()})
	default:
		err = errors.New("unsupported transactional email provider")
	}
	if err != nil {
		return err
	}
	message, err := renderer.Render(email.RenderRequest{
		Event:    outbox.Event{ID: uuid.NewString(), Type: email.ContactReminderEvent},
		Template: email.TemplateContactReminder,
		Locale:   "ar",
		Payload:  email.DeliveryPayload{Destination: parsed.Address, Locale: "ar", TemplateContract: email.TemplateContactReminder},
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), settings.Timeout()+10*time.Second)
	defer cancel()
	result, err := sender.Send(ctx, message, "gradex/contact-reminder-test/"+uuid.NewString())
	if err != nil {
		return fmt.Errorf("test send failed: %w", err)
	}
	fmt.Printf("{\n  \"test_sent\": true,\n  \"provider\": %q,\n  \"provider_message_id\": %q\n}\n", sender.Provider(), result.ProviderMessageID)
	return nil
}
