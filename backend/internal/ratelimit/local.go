package ratelimit

import (
	"sync"
	"time"
)

type localEntry struct {
	count     int64
	expiresAt time.Time
	tokens    float64
	updatedAt time.Time
}

type localFallback struct {
	mu      sync.Mutex
	entries map[string]localEntry
	now     func() time.Time
}

func newLocalFallback() *localFallback {
	return &localFallback{
		entries: make(map[string]localEntry),
		now:     time.Now,
	}
}

func (l *localFallback) decide(entries []Entry, maxKeys int) (allowed bool, available bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	for key, entry := range l.entries {
		if !now.Before(entry.expiresAt) {
			delete(l.entries, key)
		}
	}

	newKeys := 0
	for _, entry := range entries {
		if _, exists := l.entries[entry.Key]; !exists {
			newKeys++
		}
	}
	if len(l.entries)+newKeys > maxKeys {
		return false, false
	}

	allowed = true
	for _, requested := range entries {
		current := l.entries[requested.Key]
		if requested.Burst > 0 {
			if !now.Before(current.expiresAt) {
				current = localEntry{tokens: float64(requested.Burst), updatedAt: now, expiresAt: now.Add(requested.Window)}
			}
			elapsed := now.Sub(current.updatedAt)
			if elapsed > 0 {
				current.tokens = min(float64(requested.Burst), current.tokens+elapsed.Seconds()*float64(requested.Limit)/requested.Window.Seconds())
				current.updatedAt = now
			}
			if current.tokens < 1 {
				allowed = false
			} else {
				current.tokens--
			}
			current.expiresAt = now.Add(requested.Window)
			l.entries[requested.Key] = current
			continue
		}
		if !now.Before(current.expiresAt) {
			current = localEntry{expiresAt: now.Add(requested.Window)}
		}
		current.count++
		l.entries[requested.Key] = current
		if current.count > requested.Limit {
			allowed = false
		}
	}
	return allowed, true
}
