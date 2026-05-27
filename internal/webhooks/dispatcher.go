package webhooks

import "context"

// EventDispatcher is a minimal interface other packages accept to fire events
// without importing the full webhooks package (avoids import cycles if needed).
type EventDispatcher interface {
	Fire(ctx context.Context, event string, data any)
}
