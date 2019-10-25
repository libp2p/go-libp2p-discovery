package discovery

import (
	"context"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p-core/discovery"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
)

type mockDiscoveryServer struct {
	mx sync.Mutex
	db map[string]map[peer.ID]*discoveryRegistration
}

type discoveryRegistration struct {
	info       peer.AddrInfo
	expiration time.Time
}

func newDiscoveryServer() *mockDiscoveryServer {
	return &mockDiscoveryServer{
		db: make(map[string]map[peer.ID]*discoveryRegistration),
	}
}

func (s *mockDiscoveryServer) Advertise(ns string, info peer.AddrInfo, ttl time.Duration) (time.Duration, error) {
	s.mx.Lock()
	defer s.mx.Unlock()

	peers, ok := s.db[ns]
	if !ok {
		peers = make(map[peer.ID]*discoveryRegistration)
		s.db[ns] = peers
	}
	peers[info.ID] = &discoveryRegistration{info, time.Now().Add(ttl)}
	return ttl, nil
}

func (s *mockDiscoveryServer) FindPeers(ns string, limit int) (<-chan peer.AddrInfo, error) {
	s.mx.Lock()
	defer s.mx.Unlock()

	peers, ok := s.db[ns]
	if !ok || len(peers) == 0 {
		emptyCh := make(chan peer.AddrInfo)
		close(emptyCh)
		return emptyCh, nil
	}

	count := len(peers)
	if limit != 0 && count > limit {
		count = limit
	}

	iterTime := time.Now()
	ch := make(chan peer.AddrInfo, count)
	numSent := 0
	for p, reg := range peers {
		if numSent == count {
			break
		}
		if iterTime.After(reg.expiration) {
			delete(peers, p)
			continue
		}

		numSent++
		ch <- reg.info
	}
	close(ch)

	return ch, nil
}

func (s *mockDiscoveryServer) hasPeerRecord(ns string, pid peer.ID) bool {
	s.mx.Lock()
	defer s.mx.Unlock()

	if peers, ok := s.db[ns]; ok {
		_, ok := peers[pid]
		return ok
	}
	return false
}

type mockDiscoveryClient struct {
	host   host.Host
	server *mockDiscoveryServer
}

func (d *mockDiscoveryClient) Advertise(ctx context.Context, ns string, opts ...discovery.Option) (time.Duration, error) {
	var options discovery.Options
	err := options.Apply(opts...)
	if err != nil {
		return 0, err
	}

	return d.server.Advertise(ns, *host.InfoFromHost(d.host), options.Ttl)
}

func (d *mockDiscoveryClient) FindPeers(ctx context.Context, ns string, opts ...discovery.Option) (<-chan peer.AddrInfo, error) {
	var options discovery.Options
	err := options.Apply(opts...)
	if err != nil {
		return nil, err
	}

	return d.server.FindPeers(ns, options.Limit)
}
