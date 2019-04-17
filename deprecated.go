package discovery

import (
	"time"

	moved "github.com/libp2p/go-libp2p-core/discovery"
)

// Deprecated: use skel.Advertiser instead.
type Advertiser = moved.Advertiser

// Deprecated: use skel.Discoverer instead.
type Discoverer = moved.Discoverer

// Deprecated: use skel.Discovery instead.
type Discovery = moved.Discovery

// Deprecated: use github.com/libp2p/go-libp2p-core/discovery.DiscoveryOpt instead.
type Option = moved.DiscoveryOpt

// Deprecated: use github.com/libp2p/go-libp2p-core/discovery.DiscoveryOpts instead.
type Options = moved.DiscoveryOpts

// Deprecated: use github.com/libp2p/go-libp2p-core/discovery.TTL instead.
func TTL(ttl time.Duration) moved.DiscoveryOpt {
	return moved.TTL(ttl)
}

// Deprecated: use github.com/libp2p/go-libp2p-core/discovery.Limit instead.
func Limit(limit int) moved.DiscoveryOpt {
	return moved.Limit(limit)
}
