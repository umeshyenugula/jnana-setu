package chat

import (
	"sync"
	"time"
)

// Limiter is a fixed-window in-memory rate limiter keyed by an opaque caller
// key (widget + client address). It bounds anonymous chat abuse per instance.
// Distributed deployments should front this endpoint with shared rate
// limiting; this gate is the local last line of defense.
type Limiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	max    int
	window time.Duration
}

func NewLimiter(maxPerWindow int, window time.Duration) *Limiter {
	if maxPerWindow <= 0 {
		maxPerWindow = 30
	}
	if window <= 0 {
		window = time.Minute
	}
	return &Limiter{hits: map[string][]time.Time{}, max: maxPerWindow, window: window}
}

// Allow reports whether a request under key may proceed.
func (l *Limiter) Allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-l.window)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	// Opportunistic cleanup to bound memory.
	if len(l.hits) > 10000 {
		for k, v := range l.hits {
			if len(v) == 0 {
				delete(l.hits, k)
			}
		}
	}
	return true
}
