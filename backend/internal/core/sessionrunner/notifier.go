package sessionrunner

import "context"

// AllocationNotifier wakes runners waiting for work in a specific pool.
// Allocation state remains durable in Store; notifications are only hints.
type AllocationNotifier interface {
	Notify(ctx context.Context, pool string) error
	Subscribe(ctx context.Context, pool string) (<-chan struct{}, func(), error)
}
