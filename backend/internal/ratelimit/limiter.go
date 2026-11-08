package ratelimit

import (
	"context"
	"sync"
	"time"
)

// Limiter is a lightweight token bucket without background goroutines.
type Limiter struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
}

func New(rps int) *Limiter {
	if rps <= 0 {
		rps = 1
	}
	r := float64(rps)
	return &Limiter{
		rate:   r,
		burst:  r,
		tokens: r,
		last:   time.Now(),
	}
}

func (l *Limiter) Wait(ctx context.Context) error {
	for {
		l.mu.Lock()
		now := time.Now()
		elapsed := now.Sub(l.last).Seconds()
		l.tokens = min(l.burst, l.tokens+elapsed*l.rate)
		l.last = now
		if l.tokens >= 1 {
			l.tokens--
			l.mu.Unlock()
			return nil
		}
		wait := time.Duration((1 - l.tokens) / l.rate * float64(time.Second))
		l.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// ChatLimiterMap manages per-chat rate limiters (e.g. 2 answers/sec per chat).
type ChatLimiterMap struct {
	mu       sync.Mutex
	rps      int
	limiters map[int64]*Limiter
}

func NewChatLimiterMap(rps int) *ChatLimiterMap {
	return &ChatLimiterMap{
		rps:      rps,
		limiters: make(map[int64]*Limiter),
	}
}

func (m *ChatLimiterMap) Wait(ctx context.Context, chatID int64) error {
	m.mu.Lock()
	lim, ok := m.limiters[chatID]
	if !ok {
		lim = New(m.rps)
		m.limiters[chatID] = lim
	}
	m.mu.Unlock()
	return lim.Wait(ctx)
}

