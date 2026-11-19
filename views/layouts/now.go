package layouts

import (
	"context"
	"time"
)

type nowKey struct{}

// WithNow fixes the time one response renders at, so every part of a page
// (a result row, its preview, the event's history) agrees on what has
// already happened.
func WithNow(ctx context.Context, now time.Time) context.Context {
	return context.WithValue(ctx, nowKey{}, now)
}

// Now is the response's render time, or the current time outside a response.
func Now(ctx context.Context) time.Time {
	if now, ok := ctx.Value(nowKey{}).(time.Time); ok {
		return now
	}
	return time.Now()
}
