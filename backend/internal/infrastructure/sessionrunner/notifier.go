package sessionrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"github.com/redis/go-redis/v9"
)

const allocationNotifyTopicPrefix = "agentapi:session-runner-allocation:notify:"

type LocalAllocationNotifier struct {
	mu   sync.Mutex
	subs map[string]map[chan struct{}]struct{}
}

func NewLocalAllocationNotifier() *LocalAllocationNotifier {
	return &LocalAllocationNotifier{subs: make(map[string]map[chan struct{}]struct{})}
}

func (n *LocalAllocationNotifier) Notify(_ context.Context, pool string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	for ch := range n.subs[pool] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	return nil
}

func (n *LocalAllocationNotifier) Subscribe(_ context.Context, pool string) (<-chan struct{}, func(), error) {
	ch := make(chan struct{}, 1)
	n.mu.Lock()
	if n.subs[pool] == nil {
		n.subs[pool] = make(map[chan struct{}]struct{})
	}
	n.subs[pool][ch] = struct{}{}
	n.mu.Unlock()
	cancel := func() {
		n.mu.Lock()
		if subscribers := n.subs[pool]; subscribers != nil {
			if _, ok := subscribers[ch]; ok {
				delete(subscribers, ch)
				close(ch)
			}
			if len(subscribers) == 0 {
				delete(n.subs, pool)
			}
		}
		n.mu.Unlock()
	}
	return ch, cancel, nil
}

type RedisAllocationNotifier struct {
	client *redis.Client
	local  *LocalAllocationNotifier
}

func NewRedisAllocationNotifier(client *redis.Client) *RedisAllocationNotifier {
	return &RedisAllocationNotifier{client: client, local: NewLocalAllocationNotifier()}
}

func allocationNotifyTopic(pool string) string {
	digest := sha256.Sum256([]byte(pool))
	return allocationNotifyTopicPrefix + hex.EncodeToString(digest[:8])
}

func (n *RedisAllocationNotifier) Notify(ctx context.Context, pool string) error {
	_ = n.local.Notify(ctx, pool)
	if n.client == nil {
		return nil
	}
	return n.client.Publish(ctx, allocationNotifyTopic(pool), "ping").Err()
}

func (n *RedisAllocationNotifier) Subscribe(ctx context.Context, pool string) (<-chan struct{}, func(), error) {
	if n.client == nil {
		return n.local.Subscribe(ctx, pool)
	}
	localCh, localCancel, _ := n.local.Subscribe(ctx, pool)
	pubsub := n.client.Subscribe(ctx, allocationNotifyTopic(pool))
	if _, err := pubsub.Receive(ctx); err != nil {
		localCancel()
		_ = pubsub.Close()
		return nil, nil, err
	}
	out := make(chan struct{}, 1)
	done := make(chan struct{})
	var cancelOnce sync.Once
	go func() {
		defer close(out)
		redisCh := pubsub.Channel()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case _, ok := <-localCh:
				if !ok {
					localCh = nil
					continue
				}
				select {
				case out <- struct{}{}:
				default:
				}
			case _, ok := <-redisCh:
				if !ok {
					return
				}
				select {
				case out <- struct{}{}:
				default:
				}
			}
		}
	}()
	cancel := func() {
		cancelOnce.Do(func() {
			close(done)
			localCancel()
			_ = pubsub.Close()
		})
	}
	return out, cancel, nil
}
