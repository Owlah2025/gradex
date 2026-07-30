package catalogpublic

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/Owlah2025/gradex/backend/internal/problem"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPublishedOnlyContainsEveryPublicExclusion(t *testing.T) {
	const want = "c.lifecycle = 'PUBLISHED' AND c.access_suspended_at IS NULL AND c.retired_at IS NULL AND c.live_revision_id = cr.id"
	if got := PublishedOnly("c", "cr"); got != want {
		t.Fatalf("PublishedOnly() = %q, want %q", got, want)
	}
}

func TestRepositoryRefusesMissingRequiredDependencies(t *testing.T) {
	if _, err := NewRepository(nil, PublishedOnly); !errors.Is(err, ErrRepositoryNil) {
		t.Fatalf("NewRepository(nil, PublishedOnly) error = %v, want %v", err, ErrRepositoryNil)
	}
	if _, err := NewRepository(&pgxpool.Pool{}, nil); !errors.Is(err, ErrVisibilityNil) {
		t.Fatalf("NewRepository(pool, nil) error = %v, want %v", err, ErrVisibilityNil)
	}
}

func TestPublicNotFoundIsByteIdenticalForHiddenAndAbsentCourses(t *testing.T) {
	hidden := httptest.NewRecorder()
	absent := httptest.NewRecorder()
	for _, recorder := range []*httptest.ResponseRecorder{hidden, absent} {
		if err := problem.Write(recorder, NotFound()); err != nil {
			t.Fatalf("writing public not-found response: %v", err)
		}
	}

	if hidden.Code != http.StatusNotFound || absent.Code != http.StatusNotFound {
		t.Fatalf("status = (%d, %d), want both %d", hidden.Code, absent.Code, http.StatusNotFound)
	}
	if hidden.Body.String() != absent.Body.String() {
		t.Fatalf("not-found bodies differ: hidden=%q absent=%q", hidden.Body.String(), absent.Body.String())
	}
	if !reflect.DeepEqual(hidden.Header(), absent.Header()) {
		t.Fatalf("not-found headers differ: hidden=%v absent=%v", hidden.Header(), absent.Header())
	}
}
