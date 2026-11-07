// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package auth

import (
	"sync"
	"time"
)

// Limiter counts failed attempts per key (an address plus an account, say) in a sliding
// window, to slow down guessing.
type Limiter struct {
	Max    int
	Window time.Duration
	Now    func() time.Time

	mu   sync.Mutex
	hits map[string][]time.Time
}

// NewLimiter allows max failures per window.
func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{Max: max, Window: window, Now: time.Now, hits: map[string][]time.Time{}}
}

// Blocked reports whether key has used up its attempts.
func (l *Limiter) Blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.prune(key, l.Now())) >= l.Max
}

// Fail records a failed attempt.
func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.Now()
	l.hits[key] = append(l.prune(key, now), now)
	if len(l.hits) > 50_000 { // a flood of distinct keys: drop the oldest state
		l.hits = map[string][]time.Time{}
	}
}

// Reset clears a key after a successful attempt.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, key)
}

func (l *Limiter) prune(key string, now time.Time) []time.Time {
	list := l.hits[key]
	i := 0
	for i < len(list) && now.Sub(list[i]) > l.Window {
		i++
	}
	list = list[i:]
	if len(list) == 0 {
		delete(l.hits, key)
		return nil
	}
	l.hits[key] = list
	return list
}
