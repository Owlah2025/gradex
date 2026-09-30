package httpapi

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/catalogpublic"
)

type PublicCatalogFoundation struct {
	repository          *catalogpublic.Repository
	searchEventRecorder func(string, int, string)
}

const (
	publicSearchEventQueueCapacity = 128
	publicSearchEventBurst         = 20
	publicSearchEventsPerSecond    = 10
)

type publicSearchEvent struct {
	query       string
	resultCount int
	locale      string
}

type publicSearchEventQueue struct {
	events       chan publicSearchEvent
	writer       func(context.Context, string, int, string) error
	mu           sync.Mutex
	tokens       float64
	lastRefilled time.Time
}

func newPublicSearchEventQueue(writer func(context.Context, string, int, string) error) *publicSearchEventQueue {
	queue := &publicSearchEventQueue{
		events:       make(chan publicSearchEvent, publicSearchEventQueueCapacity),
		writer:       writer,
		tokens:       publicSearchEventBurst,
		lastRefilled: time.Now(),
	}
	go queue.drain()
	return queue
}

func (q *publicSearchEventQueue) enqueue(query string, resultCount int, locale string) {
	if !q.takeToken() {
		return
	}
	select {
	case q.events <- publicSearchEvent{query: query, resultCount: resultCount, locale: locale}:
	default:
	}
}

func (q *publicSearchEventQueue) takeToken() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := time.Now()
	q.tokens = minSearchEventTokens(publicSearchEventBurst, q.tokens+now.Sub(q.lastRefilled).Seconds()*publicSearchEventsPerSecond)
	q.lastRefilled = now
	if q.tokens < 1 {
		return false
	}
	q.tokens--
	return true
}

func minSearchEventTokens(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}

func (q *publicSearchEventQueue) drain() {
	for event := range q.events {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		// Search telemetry is optional; a failed insert is dropped so it cannot
		// turn an anonymous catalogue read into an availability failure.
		_ = q.writer(ctx, event.query, event.resultCount, event.locale)
		cancel()
	}
}

func (f *PublicCatalogFoundation) Repository() *catalogpublic.Repository {
	if f == nil {
		return nil
	}
	return f.repository
}

type PublicCatalogFoundationOptions struct {
	Repository        *catalogpublic.Repository
	SearchEventWriter func(context.Context, string, int, string) error
}

func NewPublicCatalogFoundation(options PublicCatalogFoundationOptions) (*PublicCatalogFoundation, error) {
	if options.Repository == nil {
		return nil, errors.New("public catalogue repository is required")
	}
	writer := options.SearchEventWriter
	if writer == nil {
		writer = options.Repository.RecordSearchEvent
	}
	queue := newPublicSearchEventQueue(writer)
	return &PublicCatalogFoundation{repository: options.Repository, searchEventRecorder: queue.enqueue}, nil
}

func WithPublicCatalogFoundation(foundation *PublicCatalogFoundation) RouterOption {
	return func(options *routerOptions) error {
		if foundation == nil || foundation.repository == nil {
			return fmt.Errorf("complete public catalogue foundation is required")
		}
		if options.publicCatalog != nil {
			return fmt.Errorf("public catalogue foundation is already configured")
		}
		options.publicCatalog = foundation
		return nil
	}
}
