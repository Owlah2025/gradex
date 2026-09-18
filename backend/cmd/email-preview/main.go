// Command email-preview serves renderer-only transactional email fixtures.
//
// It is intentionally separate from the API and worker. The command refuses to
// run outside development, binds only to loopback, uses no database or sender,
// and is not built into either production runtime image.
package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/email"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

const (
	defaultListen       = "127.0.0.1:18081"
	defaultPublicOrigin = "http://localhost:3000"
	fixtureDestination  = "preview.student@example.invalid"
	fixtureCode         = "000000"
	fixtureResetToken   = "preview-reset-token"
)

var supportedLocales = []string{"en", "ar"}

var supportedContracts = []string{
	email.TemplateVerifyEmailOTP,
	email.TemplatePasswordReset,
	email.TemplatePasswordChanged,
	email.TemplateStaffInvitation,
	email.TemplateCourseInvitation,
	email.TemplateAccessGranted,
	email.TemplateBundleGranted,
	email.TemplateInviteRejected,
	email.TemplateInviteCancelled,
	email.TemplateAccessAdjusted,
	email.TemplateAccessRevoked,
	email.TemplateDeviceTrustOTP,
	email.TemplateVerifyEmail,
}

func main() {
	if err := run(os.Args[1:], os.Getenv("APP_ENV")); err != nil {
		fmt.Fprintf(os.Stderr, "email-preview: %s\n", err)
		os.Exit(1)
	}
}

func run(args []string, appEnv string) error {
	if err := validatePreviewEnvironment(appEnv); err != nil {
		return err
	}
	flags := flag.NewFlagSet("email-preview", flag.ContinueOnError)
	listen := flags.String("listen", defaultListen, "loopback address for the preview server")
	publicOrigin := flags.String("public-origin", defaultPublicOrigin, "local public origin used for absolute assets and links")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected positional argument %q", flags.Arg(0))
	}
	if err := validateLoopbackAddress(*listen); err != nil {
		return err
	}
	renderer, err := email.NewRenderer(email.RendererOptions{
		PublicOrigin: *publicOrigin,
		FromAddress:  "preview@gradex.invalid",
		FromName:     "Gradex",
		ReplyTo:      "support@gradex.invalid",
	})
	if err != nil {
		return fmt.Errorf("building renderer: %w", err)
	}

	mux := http.NewServeMux()
	for _, contract := range supportedContracts {
		for _, locale := range supportedLocales {
			mux.HandleFunc("/email/"+contract+"/"+locale, previewHandler(renderer, contract, locale))
		}
	}
	for _, locale := range supportedLocales {
		mux.HandleFunc("/email/"+email.TemplateCourseInvitation+"/purchase/"+locale, previewHandlerWithVariant(renderer, email.TemplateCourseInvitation, locale, true))
	}
	for _, locale := range supportedLocales {
		mux.HandleFunc("/"+email.TemplateVerifyEmailOTP+"/"+locale, previewHandler(renderer, email.TemplateVerifyEmailOTP, locale))
	}
	server := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	for _, contract := range supportedContracts {
		fmt.Printf("email preview: http://%s/email/%s/en\n", *listen, contract)
		fmt.Printf("email preview: http://%s/email/%s/ar\n", *listen, contract)
	}
	fmt.Printf("email preview: http://%s/email/%s/purchase/en\n", *listen, email.TemplateCourseInvitation)
	fmt.Printf("email preview: http://%s/email/%s/purchase/ar\n", *listen, email.TemplateCourseInvitation)
	return server.ListenAndServe()
}

func validatePreviewEnvironment(appEnv string) error {
	if appEnv != "development" {
		return errors.New("APP_ENV=development is required; preview never runs in production")
	}
	return nil
}

func validateLoopbackAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() || host != "127.0.0.1" {
		return errors.New("preview listen address must be 127.0.0.1 with a valid port")
	}
	return nil
}

func previewHandler(renderer *email.Renderer, contract, locale string) http.HandlerFunc {
	return previewHandlerWithVariant(renderer, contract, locale, false)
}

func previewHandlerWithVariant(renderer *email.Renderer, contract, locale string, purchaseBacked bool) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		message, err := renderFixture(renderer, contract, locale, purchaseBacked)
		if err != nil {
			http.Error(writer, "preview rendering failed", http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = writer.Write([]byte(message.HTML))
	}
}

func renderFixture(renderer *email.Renderer, contract, locale string, purchaseBacked bool) (email.Message, error) {
	request := email.RenderRequest{
		Event: outbox.Event{
			ID:          "preview-" + contract,
			AggregateID: "preview-account",
		},
		Template: contract,
		Locale:   locale,
		Payload: email.DeliveryPayload{
			Destination:      fixtureDestination,
			Locale:           locale,
			TemplateContract: contract,
			ExpiresAt:        time.Date(2030, time.January, 2, 15, 4, 0, 0, time.UTC),
		},
	}
	switch contract {
	case email.TemplateVerifyEmailOTP:
		request.Event.Type = "identity.email_verification_code_requested"
		request.Payload.VerificationToken = fixtureCode
	case email.TemplatePasswordReset:
		request.Event.Type = "identity.password_reset_requested"
		request.Payload.VerificationToken = fixtureResetToken
	case email.TemplatePasswordChanged:
		request.Event.Type = "identity.password_reset_completed"
		request.Payload.ExpiresAt = time.Time{}
	case email.TemplateStaffInvitation:
		request.Event.Type = "identity.staff_invitation_created"
		request.Payload.VerificationToken = "preview-staff-invitation-token"
	case email.TemplateCourseInvitation:
		request.Event.Type = "access.invitation_issued"
		request.Event.AggregateID = "preview-invitation"
		request.Payload.VerificationToken = "preview-course-invitation-token"
		if purchaseBacked {
			request.Event.SafePayload = map[string]any{"purchase_backed": true}
		}
	case email.TemplateAccessGranted:
		request.Event.Type = "access.granted"
	case email.TemplateBundleGranted:
		request.Event.Type = "access.bundle_granted"
	case email.TemplateInviteRejected:
		request.Event.Type = "access.invitation_rejected"
	case email.TemplateInviteCancelled:
		request.Event.Type = "access.invitation_cancelled"
	case email.TemplateAccessAdjusted:
		request.Event.Type = "access.entitlement_adjusted"
	case email.TemplateAccessRevoked:
		request.Event.Type = "access.entitlement_revoked"
	case email.TemplateDeviceTrustOTP:
		request.Event.Type = "identity.device_trust_code_requested"
		request.Payload.VerificationToken = fixtureCode
	case email.TemplateVerifyEmail:
		request.Event.Type = "identity.email_verification_requested"
		request.Payload.VerificationToken = "preview-verification-link-token"
	default:
		return email.Message{}, fmt.Errorf("unsupported preview contract %q", contract)
	}
	return renderer.Render(request)
}
