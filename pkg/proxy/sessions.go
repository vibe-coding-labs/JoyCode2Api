package proxy

import (
	"sync"
	"sync/atomic"
)

var activeSessions sync.Map // map[string]*atomic.Int64

func TrackActive(apiKey string) func() {
	if apiKey == "" {
		return func() {}
	}
	counter, _ := activeSessions.LoadOrStore(apiKey, &atomic.Int64{})
	cnt := counter.(*atomic.Int64)
	cnt.Add(1)
	return func() { cnt.Add(-1) }
}

func GetActiveSessions(apiKey string) int64 {
	if v, ok := activeSessions.Load(apiKey); ok {
		return v.(*atomic.Int64).Load()
	}
	return 0
}
