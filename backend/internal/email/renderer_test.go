package email

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

func TestRendererCoversEveryFixedTemplateInArabicAndEnglish(t *testing.T) {
	renderer, err := NewRenderer(RendererOptions{PublicOrigin: "https://gradex.example", FromAddress: "notify@gradex.example", FromName: "Gradex", ReplyTo: "support@gradex.example"})
	if err != nil {
		t.Fatal(err)
	}
	// `credential` means the message carries a secret at all; `inLinkFragment`
	// means that secret reaches the reader inside a URL fragment. They were one
	// field until the OTP template arrived, which carries a credential and
	// deliberately has no URL to put it in.
	cases := []struct {
		eventType, template string
		credential          bool
		inLinkFragment      bool
	}{
		{"identity.email_verification_requested", TemplateVerifyEmail, true, true},
		{"identity.email_verification_code_requested", TemplateVerifyEmailOTP, true, false},
		{"identity.password_reset_requested", TemplatePasswordReset, true, true},
		{"identity.password_reset_completed", TemplatePasswordChanged, false, false},
		{"identity.staff_invitation_created", TemplateStaffInvitation, true, true},
		{"access.invitation_issued", TemplateCourseInvitation, true, true},
		{"access.granted", TemplateAccessGranted, false, false},
		{"access.invitation_rejected", TemplateInviteRejected, false, false},
		{"access.invitation_cancelled", TemplateInviteCancelled, false, false},
		{"access.entitlement_adjusted", TemplateAccessAdjusted, false, false},
		{"access.entitlement_revoked", TemplateAccessRevoked, false, false},
	}

	// The launch-critical inventory is whatever the dispatcher will actually
	// carry, so it is read from the contract map rather than restated here. A
	// new transactional message cannot reach production without production
	// rendering evidence in both locales.
	covered := make(map[string]bool, len(cases))
	for _, tc := range cases {
		covered[tc.eventType] = true
	}
	for eventType, template := range eventTemplates {
		if !covered[eventType] {
			t.Fatalf("event %q (%s) is deliverable but has no production rendering case", eventType, template)
		}
	}
	for _, tc := range cases {
		for _, locale := range []string{"ar", "en"} {
			t.Run(tc.template+"/"+locale, func(t *testing.T) {
				payload := DeliveryPayload{Destination: "student@example.com", Locale: locale, TemplateContract: tc.template}
				if tc.credential {
					payload.VerificationToken = "TOKEN_CANARY"
					payload.ExpiresAt = time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
				}
				message, err := renderer.Render(RenderRequest{Event: outbox.Event{ID: uuid.NewString(), Type: tc.eventType, AggregateID: uuid.NewString()}, Template: tc.template, Locale: locale, Payload: payload})
				if err != nil {
					t.Fatal(err)
				}
				for name, value := range map[string]string{"from": message.From, "recipient": message.Recipient, "subject": message.Subject, "text": message.Text, "html": message.HTML} {
					if strings.TrimSpace(value) == "" {
						t.Errorf("%s is empty", name)
					}
				}
				wantDir := "ltr"
				if locale == "ar" {
					wantDir = "rtl"
				}
				if !strings.Contains(message.HTML, `dir="`+wantDir+`"`) || !strings.Contains(message.HTML, `lang="`+locale+`"`) {
					t.Errorf("HTML lacks %s direction/locale", locale)
				}
				lower := strings.ToLower(message.HTML + message.Text)
				// `resend.dev` and `mailpit` would mean a production message built
				// from a sandbox sender or a local capture host.
				for _, forbidden := range []string{"tracking pixel", "unsubscribe", "utm_", "debug", "localhost", "resend.dev", "mailpit", "127.0.0.1"} {
					if strings.Contains(lower, forbidden) {
						t.Errorf("content contains forbidden %q", forbidden)
					}
				}
				// Every link a recipient can follow is built from the production
				// origin, which is where the token stays valid.
				for _, scheme := range []string{"http://", "https://"} {
					for _, link := range linksWithScheme(message.Text, scheme) {
						if !strings.HasPrefix(link, "https://gradex.example/") {
							t.Errorf("message links to %q, outside the production origin", link)
						}
					}
				}
				if tc.inLinkFragment {
					if !strings.Contains(message.Text, "#token=TOKEN_CANARY") {
						t.Error("credential is not carried in URL fragment")
					}
				}
				// No template may ever put a credential in a query string,
				// whether or not it uses a link at all: a query survives
				// referrers, proxy logs, and browser history.
				if strings.Contains(message.Text, "?token=") {
					t.Error("credential leaked into query string")
				}
				if tc.template == TemplateCourseInvitation && locale == "en" && !strings.Contains(message.Text, "grants no Course access") {
					t.Error("Course invitation does not explain preapproval state")
				}
			})
		}
	}
}

// linksWithScheme returns every whitespace-delimited token that starts a URL,
// which is how the plain-text part presents an action link.
func linksWithScheme(text, scheme string) []string {
	var links []string
	for _, field := range strings.Fields(text) {
		if strings.HasPrefix(field, scheme) {
			links = append(links, field)
		}
	}
	return links
}

func TestRendererRefusesContractConfusion(t *testing.T) {
	renderer, _ := NewRenderer(RendererOptions{PublicOrigin: "https://gradex.example", FromAddress: "notify@gradex.example", FromName: "Gradex"})
	_, err := renderer.Render(RenderRequest{Event: outbox.Event{Type: "identity.password_reset_requested"}, Template: TemplateVerifyEmail, Locale: "en", Payload: DeliveryPayload{Destination: "student@example.com", Locale: "en", TemplateContract: TemplateVerifyEmail, VerificationToken: "canary", ExpiresAt: time.Now().Add(time.Hour)}})
	if err == nil {
		t.Fatal("cross-purpose event/template pair was accepted")
	}
}

func TestRendererExplainsPurchaseBackedCourseInvitationWithoutASecondApproval(t *testing.T) {
	renderer, err := NewRenderer(RendererOptions{PublicOrigin: "https://gradex.example", FromAddress: "notify@gradex.example", FromName: "Gradex"})
	if err != nil {
		t.Fatal(err)
	}
	message, err := renderer.Render(RenderRequest{
		Event:    outbox.Event{ID: uuid.NewString(), Type: "access.invitation_issued", AggregateID: uuid.NewString(), SafePayload: map[string]any{"purchase_backed": true}},
		Template: TemplateCourseInvitation,
		Locale:   "en",
		Payload:  DeliveryPayload{Destination: "student@example.com", Locale: "en", TemplateContract: TemplateCourseInvitation, VerificationToken: "TOKEN_CANARY", ExpiresAt: time.Now().Add(time.Hour)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(message.Text, "access becomes active immediately") {
		t.Fatalf("purchase invitation claims a second approval or omits automatic access: %q", message.Text)
	}
	if strings.Contains(message.Text, "must approve") {
		t.Fatalf("purchase invitation incorrectly claims another Admin approval: %q", message.Text)
	}
}

// TestVerificationCodeMessageCarriesTheCodeAndNoLink is the production
// rendering evidence for the OTP contract. A verification message that also
// contained a URL would put two live credentials for one challenge in one
// mailbox, and the whole reason the code exists is that a forwarded link is a
// usable credential while a typed code is not.
func TestVerificationCodeMessageCarriesTheCodeAndNoLink(t *testing.T) {
	renderer, err := NewRenderer(RendererOptions{PublicOrigin: "https://gradex.example", FromAddress: "notify@gradex.example", FromName: "Gradex"})
	if err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"ar", "en"} {
		message, err := renderer.Render(RenderRequest{
			Event:    outbox.Event{ID: uuid.NewString(), Type: "identity.email_verification_code_requested", AggregateID: uuid.NewString()},
			Template: TemplateVerifyEmailOTP,
			Locale:   locale,
			Payload: DeliveryPayload{
				Destination: "student@example.com", Locale: locale,
				TemplateContract: TemplateVerifyEmailOTP, VerificationToken: "482913",
				ExpiresAt: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
			},
		})
		if err != nil {
			t.Fatalf("%s: %v", locale, err)
		}
		if !strings.Contains(message.HTML, "482913") || !strings.Contains(message.Text, "482913") {
			t.Errorf("%s: the code is not readable in the message", locale)
		}
		if strings.Contains(message.HTML, "<a href") || strings.Contains(message.HTML, "gradex.example/") {
			t.Errorf("%s: an OTP message must carry no action link", locale)
		}
		if strings.Contains(message.Text, "https://") {
			t.Errorf("%s: an OTP text part must carry no URL", locale)
		}
		// The expiry line must describe what actually expires.
		wantLabel := "Code expires:"
		if locale == "ar" {
			wantLabel = "تنتهي صلاحية الرمز:"
		}
		if !strings.Contains(message.Text, wantLabel) {
			t.Errorf("%s: expiry label does not name the code", locale)
		}
	}
}

// TestVerificationCodeMessageRefusesToRenderWithoutACode proves the render
// fails closed. Sending "your code is:" with nothing after it would burn the
// challenge for a Student who can never complete it.
func TestVerificationCodeMessageRefusesToRenderWithoutACode(t *testing.T) {
	renderer, err := NewRenderer(RendererOptions{PublicOrigin: "https://gradex.example", FromAddress: "notify@gradex.example", FromName: "Gradex"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = renderer.Render(RenderRequest{
		Event:    outbox.Event{ID: uuid.NewString(), Type: "identity.email_verification_code_requested"},
		Template: TemplateVerifyEmailOTP,
		Locale:   "en",
		Payload:  DeliveryPayload{Destination: "student@example.com", Locale: "en", TemplateContract: TemplateVerifyEmailOTP},
	})
	if err == nil {
		t.Fatal("rendering a code message with no code must fail")
	}
}
