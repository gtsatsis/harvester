// Package redis handles the monitor capabilities of harvester using redis.
package redis

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/gtsatsis/harvester/change"
	"github.com/gtsatsis/harvester/config"
	"github.com/gtsatsis/harvester/log"
)

// Watcher of Redis changes.
type Watcher struct {
	client       redis.UniversalClient
	keys         []string
	versions     []uint64
	hashes       []string
	pollInterval time.Duration
}

// New watcher.
func New(client redis.UniversalClient, pollInterval time.Duration, keys []string) (*Watcher, error) {
	if client == nil {
		return nil, errors.New("client is nil")
	}
	if pollInterval <= 0 {
		return nil, errors.New("poll interval should be a positive number")
	}
	if len(keys) == 0 {
		return nil, errors.New("keys are empty")
	}

	return &Watcher{
		client:       client,
		keys:         keys,
		versions:     make([]uint64, len(keys)),
		hashes:       make([]string, len(keys)),
		pollInterval: pollInterval,
	}, nil
}

// Watch keys and changes.
func (w *Watcher) Watch(ctx context.Context, ch chan<- []*change.Change) error {
	if ctx == nil {
		return errors.New("context is nil")
	}
	if ch == nil {
		return errors.New("change channel is nil")
	}

	go w.monitor(ctx, ch)
	return nil
}

func (w *Watcher) monitor(ctx context.Context, ch chan<- []*change.Change) {
	tickerStats := time.NewTicker(w.pollInterval)
	defer tickerStats.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tickerStats.C:
			w.getValues(ctx, ch)
		}
	}
}

func (w *Watcher) getValues(ctx context.Context, ch chan<- []*change.Change) {
	values, err := w.client.MGet(ctx, w.keys...).Result()
	if err != nil {
		log.Errorf("failed to MGET keys %v: %v", w.keys, err)
		return
	}
	changes := make([]*change.Change, 0, len(w.keys))

	for i, key := range w.keys {
		if values[i] == nil {
			continue
		}

		value := values[i].(string)
		hash := w.hash(value)
		if hash == w.hashes[i] {
			continue
		}

		w.versions[i]++
		w.hashes[i] = hash

		changes = append(changes, change.New(config.SourceRedis, key, value, w.versions[i]))
	}

	if len(changes) == 0 {
		return
	}

	ch <- changes
}

func (w *Watcher) hash(value string) string {
	hash := md5.Sum([]byte(value))
	return hex.EncodeToString(hash[:])
}
