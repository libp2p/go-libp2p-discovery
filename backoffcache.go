package discovery

import (
	"context"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p-core/discovery"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-peerstore/addr"
)

// BackoffDiscovery is an implementation of discovery that caches peer data and attenuates repeated queries
type BackoffDiscovery struct {
	disc         discovery.Discovery
	strat        BackoffFactory
	peerCache    map[string]*backoffCache
	peerCacheMux sync.RWMutex
}

func NewBackoffDiscovery(disc discovery.Discovery, strat BackoffFactory) (discovery.Discovery, error) {
	return &BackoffDiscovery{
		disc:      disc,
		strat:     strat,
		peerCache: make(map[string]*backoffCache),
	}, nil
}

type backoffCache struct {
	nextDiscover time.Time
	prevPeers    map[peer.ID]peer.AddrInfo

	peers      map[peer.ID]peer.AddrInfo
	sendingChs map[chan peer.AddrInfo]int

	ongoing bool
	strat   BackoffStrategy
	mux     sync.Mutex
}

func (d *BackoffDiscovery) Advertise(ctx context.Context, ns string, opts ...discovery.Option) (time.Duration, error) {
	return d.disc.Advertise(ctx, ns, opts...)
}

func (d *BackoffDiscovery) FindPeers(ctx context.Context, ns string, opts ...discovery.Option) (<-chan peer.AddrInfo, error) {
	// Get options
	var options discovery.Options
	err := options.Apply(opts...)
	if err != nil {
		return nil, err
	}

	// Get cached peers
	d.peerCacheMux.RLock()
	c, ok := d.peerCache[ns]
	d.peerCacheMux.RUnlock()

	/*
		Overall plan:
		If it's time to look for peers, look for peers, then return them
		If it's not time then return cache
		If it's time to look for peers, but we have already started looking. Get up to speed with ongoing request
	*/

	// Setup cache if we don't have one yet
	if !ok {
		pc := &backoffCache{
			nextDiscover: time.Time{},
			prevPeers:    make(map[peer.ID]peer.AddrInfo),
			peers:        make(map[peer.ID]peer.AddrInfo),
			sendingChs:   make(map[chan peer.AddrInfo]int),
			strat:        d.strat(),
		}
		d.peerCacheMux.Lock()
		c, ok = d.peerCache[ns]

		if !ok {
			d.peerCache[ns] = pc
			c = pc
		}

		d.peerCacheMux.Unlock()
	}

	c.mux.Lock()
	defer c.mux.Unlock()

	findPeers := !ok
	timeExpired := false
	if !findPeers {
		timeExpired = time.Now().After(c.nextDiscover)
		findPeers = timeExpired && !c.ongoing
	}

	// If we should find peers then setup a dispatcher channel for dispatching incoming peers
	if findPeers {
		pch, err := d.disc.FindPeers(ctx, ns, opts...)
		if err != nil {
			return nil, err
		}

		c.ongoing = true

		go func() {
			defer func() {
				c.mux.Lock()

				for ch := range c.sendingChs {
					close(ch)
				}

				// If the peer addresses have changed reset the backoff
				if checkUpdates(c.prevPeers, c.peers) {
					c.strat.Reset()
					c.prevPeers = c.peers
				}
				c.nextDiscover = time.Now().Add(c.strat.Delay())

				c.ongoing = false
				c.peers = make(map[peer.ID]peer.AddrInfo)
				c.sendingChs = make(map[chan peer.AddrInfo]int)
				c.mux.Unlock()
			}()

			for {
				select {
				case ai, ok := <-pch:
					if !ok {
						return
					}
					c.mux.Lock()

					// If we receive the same peer multiple times return the address union
					var sendAi peer.AddrInfo
					if prevAi, ok := c.peers[ai.ID]; ok {
						if combinedAi := mergeAddrInfos(prevAi, ai); combinedAi != nil {
							sendAi = *combinedAi
						} else {
							c.mux.Unlock()
							continue
						}
					} else {
						sendAi = ai
					}

					c.peers[ai.ID] = sendAi

					for ch, rem := range c.sendingChs {
						ch <- sendAi
						if rem == 1 {
							close(ch)
							delete(c.sendingChs, ch)
							break
						} else if rem > 0 {
							rem--
						}
					}

					c.mux.Unlock()
				case <-ctx.Done():
					return
				}
			}
		}()
		// If it's not yet time to search again then return cached peers
	} else if !timeExpired {
		chLen := options.Limit

		if chLen == 0 {
			chLen = len(c.prevPeers)
		} else if chLen > len(c.prevPeers) {
			chLen = len(c.prevPeers)
		}
		pch := make(chan peer.AddrInfo, chLen)
		for _, ai := range c.prevPeers {
			pch <- ai
		}
		close(pch)
		return pch, nil
	}

	// Setup receiver channel for receiving peers from ongoing requests

	evtCh := make(chan peer.AddrInfo, 32)
	pch := make(chan peer.AddrInfo, 8)
	rcvPeers := make([]peer.AddrInfo, 0, 32)
	for _, ai := range c.peers {
		rcvPeers = append(rcvPeers, ai)
	}
	c.sendingChs[evtCh] = options.Limit

	go func() {
		defer close(pch)

		for {
			select {
			case ai, ok := <-evtCh:
				if ok {
					rcvPeers = append(rcvPeers, ai)

					sentAll := true
				sendPeers:
					for i, p := range rcvPeers {
						select {
						case pch <- p:
						default:
							rcvPeers = rcvPeers[i:]
							sentAll = false
							break sendPeers
						}
					}
					if sentAll {
						rcvPeers = []peer.AddrInfo{}
					}
				} else {
					for _, p := range rcvPeers {
						select {
						case pch <- p:
						case <-ctx.Done():
							return
						}
					}
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return pch, nil
}

func mergeAddrInfos(prevAi, newAi peer.AddrInfo) *peer.AddrInfo {
	combinedAddrs := addr.UniqueSource(addr.Slice(prevAi.Addrs), addr.Slice(newAi.Addrs)).Addrs()
	if len(combinedAddrs) > len(prevAi.Addrs) {
		combinedAi := &peer.AddrInfo{ID: prevAi.ID, Addrs: combinedAddrs}
		return combinedAi
	}
	return nil
}

func checkUpdates(orig, update map[peer.ID]peer.AddrInfo) bool {
	if len(orig) != len(update) {
		return true
	}
	for p, ai := range update {
		if prevAi, ok := orig[p]; ok {
			if combinedAi := mergeAddrInfos(prevAi, ai); combinedAi != nil {
				return true
			}
		} else {
			return true
		}
	}
	return false
}
