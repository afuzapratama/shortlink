package store

import (
	"context"
	"time"
)

// Timing observers are injected by isolated tests, never exposed as debug HTTP endpoints.
type timingKey struct{}

func WithTiming(ctx context.Context, observe func(string, time.Duration)) context.Context {
	return context.WithValue(ctx, timingKey{}, observe)
}
func Measure(ctx context.Context, phase string) func() {
	observer, ok := ctx.Value(timingKey{}).(func(string, time.Duration))
	if !ok {
		return func() {}
	}
	began := time.Now()
	return func() { observer(phase, time.Since(began)) }
}
