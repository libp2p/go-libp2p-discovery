package discovery

import (
	"context"
	lru "github.com/hashicorp/golang-lru"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
)

type BackoffConnector struct {
	cache      *lru.TwoQueueCache
	host       host.Host
	connTryDur time.Duration
	backoff    BackoffFactory
	mux        sync.Mutex
}

func NewBackoffConnector(h host.Host, cacheSize int, connectionTryDuration time.Duration, backoff BackoffFactory) (*BackoffConnector, error) {
	cache, err := lru.New2Q(cacheSize)
	if err != nil {
		return nil, err
	}

	return &BackoffConnector{
		cache:      cache,
		host:       h,
		connTryDur: connectionTryDuration,
		backoff:    backoff,
	}, nil
}

type connCacheData struct {
	nextTry time.Time
	strat   BackoffStrategy
}

func (c *BackoffConnector) Connect(ctx context.Context, peerCh <-chan peer.AddrInfo) {
	for {
		select {
		case pi, ok := <-peerCh:
			if !ok {
				return
			}

			if pi.ID == c.host.ID() || pi.ID == "" {
				continue
			}

			c.mux.Lock()
			val, ok := c.cache.Get(pi.ID)
			var cachedPeer *connCacheData
			if ok {
				tv := val.(*connCacheData)
				now := time.Now()
				if now.Before(tv.nextTry) {
					c.mux.Unlock()
					continue
				}

				tv.nextTry = now.Add(tv.strat.Delay())
			} else {
				cachedPeer = &connCacheData{strat: c.backoff()}
				cachedPeer.nextTry = time.Now().Add(cachedPeer.strat.Delay())
				c.cache.Add(pi.ID, cachedPeer)
			}
			c.mux.Unlock()

			go func(pi peer.AddrInfo) {
				ctx, cancel := context.WithTimeout(ctx, c.connTryDur)
				defer cancel()

				err := c.host.Connect(ctx, pi)
				if err != nil {
					log.Debugf("Error connecting to pubsub peer %s: %s", pi.ID, err.Error())
					return
				}
			}(pi)

		case <-ctx.Done():
			log.Infof("discovery: backoff connector context error %v", ctx.Err())
			return
		}
	}
}
