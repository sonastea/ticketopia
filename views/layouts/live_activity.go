package layouts

import "context"

type liveActivityKey struct{}

func WithLiveActivity(ctx context.Context) context.Context {
	return context.WithValue(ctx, liveActivityKey{}, true)
}
func LiveActivityEnabled(ctx context.Context) bool {
	enabled, _ := ctx.Value(liveActivityKey{}).(bool)
	return enabled
}
