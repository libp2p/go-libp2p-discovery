package discovery

import (
	"context"
	"math/rand"
	"testing"
	"time"

	bhost "github.com/libp2p/go-libp2p-blankhost"
	"github.com/libp2p/go-libp2p-core/discovery"
	"github.com/libp2p/go-libp2p-core/peer"
	swarmt "github.com/libp2p/go-libp2p-swarm/testing"
)

type delayedDiscovery struct {
	disc  discovery.Discovery
	delay time.Duration
}

func (d *delayedDiscovery) Advertise(ctx context.Context, ns string, opts ...discovery.Option) (time.Duration, error) {
	return d.disc.Advertise(ctx, ns, opts...)
}

func (d *delayedDiscovery) FindPeers(ctx context.Context, ns string, opts ...discovery.Option) (<-chan peer.AddrInfo, error) {
	dch, err := d.disc.FindPeers(ctx, ns, opts...)
	if err != nil {
		return nil, err
	}

	ch := make(chan peer.AddrInfo, 32)
	go func() {
		defer close(ch)
		for ai := range dch {
			ch <- ai
			time.Sleep(d.delay)
		}
	}()

	return ch, nil
}

func assertNumPeers(t *testing.T, ctx context.Context, d discovery.Discovery, ns string, count int) {
	t.Helper()
	peerCh, err := d.FindPeers(ctx, ns, discovery.Limit(10))
	if err != nil {
		t.Fatal(err)
	}

	peerset := make(map[peer.ID]struct{})
	for p := range peerCh {
		peerset[p.ID] = struct{}{}
	}

	if len(peerset) != count {
		t.Fatalf("Was supposed to find %d, found %d instead", count, len(peerset))
	}
}

func TestBackoffDiscoverySingleBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	discServer := newDiscoveryServer()

	h1 := bhost.NewBlankHost(swarmt.GenSwarm(t, ctx))
	h2 := bhost.NewBlankHost(swarmt.GenSwarm(t, ctx))
	d1 := &mockDiscoveryClient{h1, discServer}
	d2 := &mockDiscoveryClient{h2, discServer}

	bkf := NewExponentialBackoff(time.Millisecond*100, time.Second*10, NoJitter,
		time.Millisecond*100, 2.5, 0, rand.NewSource(0))
	dCache, err := NewBackoffDiscovery(d1, bkf)
	if err != nil {
		t.Fatal(err)
	}

	const ns = "test"

	// try adding a peer then find it
	d1.Advertise(ctx, ns, discovery.TTL(time.Hour))
	assertNumPeers(t, ctx, dCache, ns, 1)

	// add a new peer and make sure it is still hidden by the caching layer
	d2.Advertise(ctx, ns, discovery.TTL(time.Hour))
	assertNumPeers(t, ctx, dCache, ns, 1)

	// wait for cache to expire and check for the new peer
	time.Sleep(time.Millisecond * 110)
	assertNumPeers(t, ctx, dCache, ns, 2)
}

func TestBackoffDiscoveryMultipleBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	discServer := newDiscoveryServer()

	h1 := bhost.NewBlankHost(swarmt.GenSwarm(t, ctx))
	h2 := bhost.NewBlankHost(swarmt.GenSwarm(t, ctx))
	d1 := &mockDiscoveryClient{h1, discServer}
	d2 := &mockDiscoveryClient{h2, discServer}

	// Startup delay is 0ms. First backoff after finding data is 100ms, second backoff is 250ms.
	bkf := NewExponentialBackoff(time.Millisecond*100, time.Second*10, NoJitter,
		time.Millisecond*100, 2.5, 0, rand.NewSource(0))
	dCache, err := NewBackoffDiscovery(d1, bkf)
	if err != nil {
		t.Fatal(err)
	}

	const ns = "test"

	// try adding a peer then find it
	d1.Advertise(ctx, ns, discovery.TTL(time.Hour))
	assertNumPeers(t, ctx, dCache, ns, 1)

	// wait a little to make sure the extra request doesn't modify the backoff
	time.Sleep(time.Millisecond * 50) //50 < 100
	assertNumPeers(t, ctx, dCache, ns, 1)

	// wait for backoff to expire and check if we increase it
	time.Sleep(time.Millisecond * 60) // 50+60 > 100
	assertNumPeers(t, ctx, dCache, ns, 1)

	d2.Advertise(ctx, ns, discovery.TTL(time.Millisecond*400))

	time.Sleep(time.Millisecond * 150) //150 < 250
	assertNumPeers(t, ctx, dCache, ns, 1)

	time.Sleep(time.Millisecond * 150) //150 + 150 > 250
	assertNumPeers(t, ctx, dCache, ns, 2)

	// check that the backoff has been reset
	// also checks that we can decrease our peer count (i.e. not just growing a set)
	time.Sleep(time.Millisecond * 110) //110 > 100, also 150+150+110>400
	assertNumPeers(t, ctx, dCache, ns, 1)
}

func TestBackoffDiscoverySimultaneousQuery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	discServer := newDiscoveryServer()

	// Testing with n larger than most internal buffer sizes (32)
	n := 40
	advertisers := make([]discovery.Discovery, n)

	for i := 0; i < n; i++ {
		h := bhost.NewBlankHost(swarmt.GenSwarm(t, ctx))
		advertisers[i] = &mockDiscoveryClient{h, discServer}
	}

	d1 := &delayedDiscovery{advertisers[0], time.Millisecond * 10}

	bkf := NewFixedBackoff(time.Millisecond * 200)
	dCache, err := NewBackoffDiscovery(d1, bkf)
	if err != nil {
		t.Fatal(err)
	}

	const ns = "test"

	for _, a := range advertisers {
		if _, err := a.Advertise(ctx, ns, discovery.TTL(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}

	ch1, err := dCache.FindPeers(ctx, ns)
	if err != nil {
		t.Fatal(err)
	}

	_ = <-ch1
	ch2, err := dCache.FindPeers(ctx, ns)
	if err != nil {
		t.Fatal(err)
	}

	szCh2 := 0
	for ai := range ch2 {
		_ = ai
		szCh2++
	}

	szCh1 := 1
	for _ = range ch1 {
		szCh1++
	}

	if szCh1 != n && szCh2 != n {
		t.Fatalf("Channels returned %d, %d elements instead of %d", szCh1, szCh2, n)
	}
}
