// Command gradex-contact-reminder queues the D-118 WhatsApp contact reminder
// email for every ACTIVE Student with a verified email address.
//
// It is an operator command, run inside the API container so it uses that
// container's configuration and outbox key:
//
//	gradex-contact-reminder -campaign whatsapp-contact-2026-10            # dry run: counts only
//	gradex-contact-reminder -campaign whatsapp-contact-2026-10 -send      # queue the emails
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
	"os"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/db"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

func main() {
	campaign := flag.String("campaign", "", "campaign name, 3-64 lowercase letters, digits or hyphens (required)")
	send := flag.Bool("send", false, "queue the emails; without it the command only counts")
	flag.Parse()
	if err := run(*campaign, *send); err != nil {
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
