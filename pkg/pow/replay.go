package pow

import (
	"math/big"
	"sync"
	"time"
)

const sweepInterval = time.Minute

type replayStore struct {
	mu    sync.Mutex
	seen  map[string]time.Time
	swept time.Time
}

func newReplayStore() *replayStore {
	return &replayStore{seen: make(map[string]time.Time)}
}

func (r *replayStore) claim(challenge string, expires, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.sweep(now)
	if _, used := r.seen[challenge]; used {
		return false
	}
	r.seen[challenge] = expires
	return true
}

func (r *replayStore) sweep(now time.Time) {
	if now.Sub(r.swept) < sweepInterval {
		return
	}
	r.swept = now
	for k, exp := range r.seen {
		if now.After(exp) {
			delete(r.seen, k)
		}
	}
}

func bigMax(n int64) *big.Int {
	return big.NewInt(n + 1)
}
