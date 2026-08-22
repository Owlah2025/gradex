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
	cases := []struct {
		eventType, template string
		credential          bool
	}{
		{"identity.email_verification_requested", TemplateVerifyEmail, true}, {"identity.password_reset_requested", TemplatePasswordReset, true},
		{"identity.password_reset_completed", TemplatePasswordChanged, false}, {"identity.staff_invitation_created", TemplateStaffInvitation, true},
		{"access.invitation_issued", TemplateCourseInvitation, true}, {"access.granted", TemplateAccessGranted, false},
		{"access.invitation_rejected", TemplateInviteRejected, false}, {"access.invitation_cancelled", TemplateInviteCancelled, false},
		{"access.entitlement_adjusted", TemplateAccessAdjusted, false}, {"access.entitlement_revoked", TemplateAccessRevoked, false},
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
				if tc.credential {
					if !strings.Contains(message.Text, "#token=TOKEN_CANARY") {
						t.Error("credential is not carried in URL fragment")
					}
					if strings.Contains(message.Text, "?token=") {
						t.Error("credential leaked into query string")
					}
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
