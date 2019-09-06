package discovery

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ipfs/go-cid"
	bhost "github.com/libp2p/go-libp2p-blankhost"
	"github.com/libp2p/go-libp2p-core/discovery"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	swarmt "github.com/libp2p/go-libp2p-swarm/testing"
)

type mockRoutingTable struct {
	mx        sync.Mutex
	providers map[string]map[peer.ID]peer.AddrInfo
}

type mockRouting struct {
	h   host.Host
	tab *mockRoutingTable
}

func NewMockRoutingTable() *mockRoutingTable {
	return &mockRoutingTable{providers: make(map[string]map[peer.ID]peer.AddrInfo)}
}

func NewMockRouting(h host.Host, tab *mockRoutingTable) *mockRouting {
	return &mockRouting{h: h, tab: tab}
}

func (m *mockRouting) Provide(ctx context.Context, cid cid.Cid, bcast bool) error {
	m.tab.mx.Lock()
	defer m.tab.mx.Unlock()

	pmap, ok := m.tab.providers[cid.String()]
	if !ok {
		pmap = make(map[peer.ID]peer.AddrInfo)
		m.tab.providers[cid.String()] = pmap
	}

	pmap[m.h.ID()] = peer.AddrInfo{ID: m.h.ID(), Addrs: m.h.Addrs()}

	return nil
}

func (m *mockRouting) FindProvidersAsync(ctx context.Context, cid cid.Cid, limit int) <-chan peer.AddrInfo {
	ch := make(chan peer.AddrInfo)
	go func() {
		defer close(ch)
		m.tab.mx.Lock()
		defer m.tab.mx.Unlock()

		pmap, ok := m.tab.providers[cid.String()]
		if !ok {
			return
		}

		for _, pi := range pmap {
			select {
			case ch <- pi:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch
}

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

func TestRoutingDiscovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h1 := bhost.NewBlankHost(swarmt.GenSwarm(t, ctx))
	h2 := bhost.NewBlankHost(swarmt.GenSwarm(t, ctx))

	mtab := NewMockRoutingTable()
	mr1 := NewMockRouting(h1, mtab)
	mr2 := NewMockRouting(h2, mtab)

	d1 := NewRoutingDiscovery(mr1)
	d2 := NewRoutingDiscovery(mr2)

	_, err := d1.Advertise(ctx, "/test")
	if err != nil {
		t.Fatal(err)
	}

	pis, err := FindPeers(ctx, d2, "/test", Limit(20))
	if err != nil {
		t.Fatal(err)
	}

	if len(pis) != 1 {
		t.Fatalf("Expected 1 peer, got %d", len(pis))
	}

	pi := pis[0]
	if pi.ID != h1.ID() {
		t.Fatalf("Unexpected peer: %s", pi.ID)
	}
}

func TestDiscoveryRouting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h1 := bhost.NewBlankHost(swarmt.GenSwarm(t, ctx))
	h2 := bhost.NewBlankHost(swarmt.GenSwarm(t, ctx))

	dserver := newDiscoveryServer()
	d1 := &mockDiscoveryClient{h1, dserver}
	d2 := &mockDiscoveryClient{h2, dserver}

	r1 := NewDiscoveryRouting(d1, discovery.TTL(time.Hour))
	r2 := NewDiscoveryRouting(d2, discovery.TTL(time.Hour))

	c, err := nsToCid("/test")
	if err != nil {
		t.Fatal(err)
	}

	if err := r1.Provide(ctx, c, true); err != nil {
		t.Fatal(err)
	}

	pch := r2.FindProvidersAsync(ctx, c, 20)

	var allAIs []peer.AddrInfo
	for ai := range pch {
		allAIs = append(allAIs, ai)
	}

	if len(allAIs) != 1 {
		t.Fatalf("Expected 1 peer, got %d", len(allAIs))
	}

	ai := allAIs[0]
	if ai.ID != h1.ID() {
		t.Fatalf("Unexpected peer: %s", ai.ID)
	}
}
