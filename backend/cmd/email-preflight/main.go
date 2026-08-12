// Command email-preflight reports whether this deployment's transactional
// email configuration can actually carry launch traffic.
//
// It exists because two of LG-018's acceptance conditions cannot be asserted
// from inside the repository. Whether `EMAIL_PROVIDER=resend`, a real sender
// domain, and an HTTPS public origin are configured is a question the
// configuration boundary already answers at startup. Whether the provider
// considers that sending domain *verified* — DNS present, DKIM signing, SPF
// aligned — is a question only the provider can answer, and getting it wrong
// means mail is accepted by Gradex and then silently refused or spam-filed.
//
// This command answers both, sends no mail, mutates nothing, and prints no
// secret. It is a release gate check, not a runtime dependency: the API and
// worker never call it, so a provider outage cannot stop Gradex from starting.
//
// Usage:
//
//	go run ./cmd/email-preflight            # exit 0 only when launch-ready
//	go run ./cmd/email-preflight -offline   # configuration only, no provider call
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/config"
	transactionalemail "github.com/Owlah2025/gradex/backend/internal/email"
)

const commandTimeout = 30 * time.Second

func main() {
	scrub := newScrubber()
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "email-preflight: %s\n", scrub(err.Error()))
		os.Exit(1)
	}
}

// newScrubber keeps the API key and database password out of anything on its
// way to output, the same defense cmd/bootstrap-admin and cmd/migrate use.
// The key is held in a config.Secret that redacts itself under every
// formatting verb; this catches a library that echoes a value back.
func newScrubber() func(string) string {
	var secrets []string
	for _, key := range []string{"EMAIL_API_KEY", "DATABASE_URL"} {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		secrets = append(secrets, value)
		if parsed, err := url.Parse(value); err == nil && parsed.User != nil {
			if password, set := parsed.User.Password(); set && password != "" {
				secrets = append(secrets, password)
			}
		}
	}
	return func(message string) string {
		for _, secret := range secrets {
			message = strings.ReplaceAll(message, secret, "[REDACTED]")
		}
		return message
	}
}

func run(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("email-preflight", flag.ContinueOnError)
	flags.SetOutput(stdout)
	offline := flags.Bool("offline", false, "check configuration only; do not contact the provider")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected positional argument %q", flags.Arg(0))
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}
	settings := cfg.Email()

	fmt.Fprintf(stdout, "environment: %s\n", cfg.Environment())
	fmt.Fprintf(stdout, "email enabled: %t\n", settings.Enabled())
	if !settings.Enabled() {
		return fmt.Errorf("transactional email is disabled: %s", settings.Reason())
	}
	fmt.Fprintf(stdout, "provider: %s\n", settings.Provider())
	fmt.Fprintf(stdout, "sender: %s <%s>\n", settings.FromName(), settings.FromAddress())
	if settings.ReplyTo() != "" {
		fmt.Fprintf(stdout, "reply-to: %s\n", settings.ReplyTo())
	}
	fmt.Fprintf(stdout, "public origin: %s\n", cfg.PublicOrigin())
	fmt.Fprintf(stdout, "provider timeout: %s\n", settings.Timeout())
	fmt.Fprintf(stdout, "retry budget: %s (provider idempotency window %s)\n",
		transactionalemail.RetryBudget(), transactionalemail.ProviderIdempotencyWindow)

	if settings.Provider() != config.EmailProviderResend {
		return fmt.Errorf("production delivery requires EMAIL_PROVIDER=resend; this deployment uses %q", settings.Provider())
	}

	domain, err := transactionalemail.SenderDomain(settings.FromAddress())
	if err != nil {
		return fmt.Errorf("reading sender domain: %w", err)
	}
	fmt.Fprintf(stdout, "sending domain: %s\n", domain)

	if *offline {
		fmt.Fprintln(stdout, "result: CONFIGURATION OK — provider verification not checked (-offline)")
		return nil
	}

	inspector, err := transactionalemail.NewSendingDomainInspector(transactionalemail.SendingDomainInspectorOptions{
		APIKey:  settings.APIKey(),
		Timeout: settings.Timeout(),
	})
	if err != nil {
		return fmt.Errorf("building provider client: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	status, err := inspector.Inspect(ctx, settings.FromAddress())
	if err != nil {
		return fmt.Errorf("checking sending domain: %w", err)
	}
	if !status.Known {
		return errors.New("the sending domain is not present in the Resend account; add and verify it before launch")
	}
	fmt.Fprintf(stdout, "provider domain status: %s\n", status.Status)
	if status.Region != "" {
		fmt.Fprintf(stdout, "provider region: %s\n", status.Region)
	}
	if !status.Verified {
		return fmt.Errorf("the sending domain is %q, not verified; complete the DNS records Resend generated", status.Status)
	}

	fmt.Fprintln(stdout, "result: LAUNCH READY — provider reports the Gradex sending domain verified")
	return nil
}
