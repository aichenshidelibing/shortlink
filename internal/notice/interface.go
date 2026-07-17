package notice

import "context"

type Notifier interface {
	Name() string
	Send(ctx context.Context, message string) error
}
