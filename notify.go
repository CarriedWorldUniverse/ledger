package ledger

import "context"

// Notifier delivers a chat DM to an aspect. The issues service calls
// it on assignment, mention, and watcher-relevant transitions.
//
// Notifications are fire-and-forget — errors are logged but do not
// fail the triggering mutation.
type Notifier interface {
	NotifyAspect(ctx context.Context, aspect, message string) error
	NotifyOperatorStream(ctx context.Context, message string) error
}

// nopNotifier is the default — used in tests and when no broker is wired.
type nopNotifier struct{}

func (nopNotifier) NotifyAspect(ctx context.Context, aspect, message string) error { return nil }
func (nopNotifier) NotifyOperatorStream(ctx context.Context, message string) error  { return nil }
