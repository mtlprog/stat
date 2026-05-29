package notify

import "context"

// Provider sends a notification report to some external channel.
type Provider interface {
	Send(ctx context.Context, report Report) error
}
