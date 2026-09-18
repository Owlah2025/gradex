package main

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Owlah2025/gradex/backend/internal/email"
)

func TestPreviewRequiresDevelopment(t *testing.T) {
	if err := validatePreviewEnvironment("production"); err == nil {
		t.Fatal("production preview was accepted")
	}
	if err := validatePreviewEnvironment("development"); err != nil {
		t.Fatalf("development preview was refused: %v", err)
	}
}

func TestPreviewAcceptsOnlyLoopbackIPv4(t *testing.T) {
	for _, address := range []string{"127.0.0.1:18081"} {
		if err := validateLoopbackAddress(address); err != nil {
			t.Fatalf("%s refused: %v", address, err)
		}
	}
	for _, address := range []string{"0.0.0.0:18081", "[::1]:18081", "localhost:18081"} {
		if err := validateLoopbackAddress(address); err == nil {
			t.Fatalf("non-127.0.0.1 address %s accepted", address)
		}
	}
}

func TestPreviewRendersEverySupportedContractInBothLocales(t *testing.T) {
	renderer, err := email.NewRenderer(email.RendererOptions{
		PublicOrigin: "http://localhost:3000",
		FromAddress:  "preview@gradex.invalid",
		FromName:     "Gradex",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(supportedContracts) != 13 {
		t.Fatalf("supported preview contract count = %d, want 13", len(supportedContracts))
	}
	for _, contract := range supportedContracts {
		for _, locale := range []string{"en", "ar"} {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest("GET", "/email/"+contract+"/"+locale, nil)
			previewHandler(renderer, contract, locale)(recorder, request)
			if recorder.Code != 200 {
				t.Fatalf("%s/%s status = %d", contract, locale, recorder.Code)
			}
			html := recorder.Body.String()
			message, err := renderFixture(renderer, contract, locale, false)
			if err != nil {
				t.Fatalf("%s/%s render: %v", contract, locale, err)
			}
			if strings.TrimSpace(message.HTML) == "" || strings.TrimSpace(message.Text) == "" {
				t.Fatalf("%s/%s preview has an empty HTML or plaintext part", contract, locale)
			}
			if !strings.Contains(html, `alt="GradeX"`) {
				t.Fatalf("%s/%s preview omitted the logo fallback", contract, locale)
			}
			withCTA := contract != email.TemplateVerifyEmailOTP && contract != email.TemplateDeviceTrustOTP
			if strings.Contains(html, "<a href") != withCTA {
				t.Fatalf("%s/%s CTA presence = %t, want %t", contract, locale, strings.Contains(html, "<a href"), withCTA)
			}
			if strings.Contains(html, "If the button does not work") || strings.Contains(html, "إذا لم يعمل الزر") {
				t.Fatalf("%s/%s preview contains a visible fallback URL block", contract, locale)
			}
			if !withCTA && strings.Contains(message.Text, "http://") {
				t.Fatalf("%s/%s plaintext unexpectedly contains an action URL", contract, locale)
			}
			wantDirection := `dir="ltr"`
			if locale == "ar" {
				wantDirection = `dir="rtl"`
			}
			if !strings.Contains(html, wantDirection) {
				t.Fatalf("%s/%s preview has no %s document direction", contract, locale, wantDirection)
			}
		}
	}
	for _, locale := range []string{"en", "ar"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest("GET", "/email/"+email.TemplateCourseInvitation+"/purchase/"+locale, nil)
		previewHandlerWithVariant(renderer, email.TemplateCourseInvitation, locale, true)(recorder, request)
		if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), "Course access becomes active immediately") && locale == "en" {
			t.Fatalf("purchase-backed Course invitation preview failed for %s", locale)
		}
	}
}
