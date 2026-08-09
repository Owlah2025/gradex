package identity

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHIBPSourceScreensExactPasswordWithPrefixOnlyRequest(t *testing.T) {
	password := " Correct horse Battery staple 9 "
	fullDigest := sha1.Sum([]byte(password)) // #nosec G505 -- provider protocol fixture
	encoded := strings.ToUpper(hex.EncodeToString(fullDigest[:]))
	prefix, suffix := encoded[:hibpPrefixLength], encoded[hibpPrefixLength:]

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/range/"+prefix || r.URL.RawQuery != "" {
			t.Errorf("request = %s %s, want prefix-only range GET", r.Method, r.URL.RequestURI())
		}
		if r.Header.Get("Add-Padding") != "true" {
			t.Errorf("Add-Padding = %q, want true", r.Header.Get("Add-Padding"))
		}
		if r.Header.Get("User-Agent") != hibpUserAgent {
			t.Errorf("User-Agent = %q, want %q", r.Header.Get("User-Agent"), hibpUserAgent)
		}
		if r.ContentLength > 0 {
			t.Errorf("request ContentLength = %d, want no body", r.ContentLength)
		}
		wire := r.URL.String() + fmt.Sprint(r.Header)
		for _, forbidden := range []string{password, encoded, suffix, "student@example.com"} {
			if strings.Contains(wire, forbidden) {
				t.Errorf("request exposed forbidden credential material")
			}
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "%s:7\r\n%s:0\r\n", suffix, strings.Repeat("F", hibpSuffixLength))
	}))
	defer server.Close()

	source, err := newHIBPCompromisedSource(server.URL+"/range", server.Client())
	if err != nil {
		t.Fatalf("constructing test HIBP source: %v", err)
	}
	err = screenCompromised(context.Background(), password, source)
	if !errors.Is(err, ErrPasswordPolicy) {
		t.Fatalf("screening error = %v, want password-policy rejection", err)
	}
}

func TestHIBPSourceIgnoresZeroCountPaddingMatch(t *testing.T) {
	password := "another unique launch credential 47"
	fullDigest := sha1.Sum([]byte(password)) // #nosec G505 -- provider protocol fixture
	encoded := strings.ToUpper(hex.EncodeToString(fullDigest[:]))

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "%s:0\n", encoded[hibpPrefixLength:])
	}))
	defer server.Close()
	source, err := newHIBPCompromisedSource(server.URL+"/range", server.Client())
	if err != nil {
		t.Fatalf("constructing test HIBP source: %v", err)
	}

	if err := screenCompromised(context.Background(), password, source); err != nil {
		t.Fatalf("zero-count padding rejected password: %v", err)
	}
}

func TestHIBPSourceFailsClosedOnInvalidProviderResponses(t *testing.T) {
	tests := map[string]func(http.ResponseWriter){
		"non-success status": func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("provider-detail-canary"))
		},
		"wrong content type": func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"suffix":"provider-detail-canary"}`))
		},
		"malformed line": func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("provider-detail-canary"))
		},
		"oversized body": func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(strings.Repeat("A", hibpMaxResponseSize+1)))
		},
		"empty body": func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "text/plain")
		},
	}

	for name, respond := range tests {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				respond(w)
			}))
			defer server.Close()
			source, err := newHIBPCompromisedSource(server.URL+"/range", server.Client())
			if err != nil {
				t.Fatalf("constructing test HIBP source: %v", err)
			}

			_, err = source.Lookup(context.Background(), CompromisedRangeLookup{
				Scheme: CompromisedSHA1V1,
				Prefix: "ABCDE",
			})
			if err == nil {
				t.Fatal("invalid provider response was accepted")
			}
			for _, forbidden := range []string{"ABCDE", "provider-detail-canary"} {
				if strings.Contains(err.Error(), forbidden) {
					t.Errorf("safe error exposed %q: %v", forbidden, err)
				}
			}
		})
	}
}

func TestHIBPSourceUsesOneBoundedRequestAndFailsClosed(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		<-r.Context().Done()
	}))
	defer server.Close()
	source, err := newHIBPCompromisedSource(server.URL+"/range", server.Client())
	if err != nil {
		t.Fatalf("constructing test HIBP source: %v", err)
	}
	bounded, err := NewTimeoutCompromisedSource(source, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("constructing bounded source: %v", err)
	}

	_, err = bounded.Lookup(context.Background(), CompromisedRangeLookup{
		Scheme: CompromisedSHA1V1,
		Prefix: "ABCDE",
	})
	if err == nil {
		t.Fatal("timed-out provider lookup was treated as safe")
	}
	if requests.Load() != 1 {
		t.Fatalf("provider requests = %d, want exactly one", requests.Load())
	}
}

func TestHIBPSourceRefusesRedirects(t *testing.T) {
	var redirectedRequests atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirectedRequests.Add(1)
	}))
	defer target.Close()
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL+"/range/ABCDE")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	source, err := newHIBPCompromisedSource(origin.URL+"/range", origin.Client())
	if err != nil {
		t.Fatalf("constructing test HIBP source: %v", err)
	}

	_, err = source.Lookup(context.Background(), CompromisedRangeLookup{
		Scheme: CompromisedSHA1V1,
		Prefix: "ABCDE",
	})
	if err == nil {
		t.Fatal("provider redirect was followed")
	}
	if redirectedRequests.Load() != 0 {
		t.Fatalf("redirect target received %d requests", redirectedRequests.Load())
	}
}

func TestHIBPSourceRejectsUnsafeConfigurationAndLookupWithoutNetwork(t *testing.T) {
	for name, endpoint := range map[string]string{
		"plain HTTP":  "http://api.example/range",
		"credentials": "https://user:secret@api.example/range",
		"query":       "https://api.example/range?secret=value",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := newHIBPCompromisedSource(endpoint, &http.Client{}); err == nil {
				t.Fatal("unsafe endpoint was accepted")
			}
		})
	}

	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	source, err := newHIBPCompromisedSource(server.URL+"/range", server.Client())
	if err != nil {
		t.Fatalf("constructing test HIBP source: %v", err)
	}
	for _, lookup := range []CompromisedRangeLookup{
		{Scheme: CompromisedSHA256V1, Prefix: "ABCDE"},
		{Scheme: CompromisedSHA1V1, Prefix: "ABCD"},
		{Scheme: CompromisedSHA1V1, Prefix: "abcde"},
	} {
		if _, err := source.Lookup(context.Background(), lookup); err == nil {
			t.Errorf("invalid lookup %+v was accepted", lookup)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("invalid lookups made %d network requests", requests.Load())
	}
}
