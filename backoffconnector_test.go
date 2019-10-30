package discovery

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	bhost "github.com/libp2p/go-libp2p-blankhost"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	swarmt "github.com/libp2p/go-libp2p-swarm/testing"
)

type maxDialHost struct {
	host.Host

	mux            sync.Mutex
	timesDialed    map[peer.ID]int
	maxTimesToDial map[peer.ID]int
}

func (h *maxDialHost) Connect(ctx context.Context, ai peer.AddrInfo) error {
	pid := ai.ID

	h.mux.Lock()
	defer h.mux.Unlock()
	numDials := h.timesDialed[pid]
	numDials += 1
	h.timesDialed[pid] = numDials

	if maxDials, ok := h.maxTimesToDial[pid]; ok && numDials > maxDials {
		return fmt.Errorf("should not be dialing peer %s", pid.String())
	}

	return h.Host.Connect(ctx, ai)
}

func getNetHosts(t *testing.T, ctx context.Context, n int) []host.Host {
	var out []host.Host

	for i := 0; i < n; i++ {
		netw := swarmt.GenSwarm(t, ctx)
		h := bhost.NewBlankHost(netw)
		out = append(out, h)
	}

	return out
}

func loadCh(peers []host.Host) <-chan peer.AddrInfo {
	ch := make(chan peer.AddrInfo, len(peers))
	for _, p := range peers {
		ch <- p.Peerstore().PeerInfo(p.ID())
	}
	close(ch)
	return ch
}

func TestBackoffConnector(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hosts := getNetHosts(t, ctx, 5)
	primary := &maxDialHost{
		Host:        hosts[0],
		mux:         sync.Mutex{},
		timesDialed: make(map[peer.ID]int),
		maxTimesToDial: map[peer.ID]int{
			hosts[1].ID(): 1,
			hosts[2].ID(): 2,
		},
	}

	bc, err := NewBackoffConnector(primary, 10, time.Minute, NewFixedBackoff(time.Millisecond*1500))
	if err != nil {
		t.Fatal(err)
	}

	bc.Connect(ctx, loadCh(hosts))

	time.Sleep(time.Millisecond * 100)
	if expected, actual := len(hosts)-1, len(primary.Network().Conns()); actual != expected {
		t.Fatalf("wrong number of connections. expected %d, actual %d", expected, actual)
	}

	for _, c := range primary.Network().Conns() {
		c.Close()
	}

	for len(primary.Network().Conns()) > 0 {
		time.Sleep(time.Millisecond * 100)
	}

	bc.Connect(ctx, loadCh(hosts))
	if numConns := len(primary.Network().Conns()); numConns != 0 {
		t.Fatal("shouldn't be connected to any peers")
	}

	time.Sleep(time.Millisecond * 1600)
	bc.Connect(ctx, loadCh(hosts))

	time.Sleep(time.Millisecond * 100)
	if expected, actual := len(hosts)-2, len(primary.Network().Conns()); actual != expected {
		t.Fatalf("wrong number of connections. expected %d, actual %d", expected, actual)
	}
}
