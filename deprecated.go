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

// Deprecated: use github.com/libp2p/go-libp2p-core/discovery.Option instead.
type Option = moved.Option

// Deprecated: use github.com/libp2p/go-libp2p-core/discovery.Options instead.
type Options = moved.Options

// Deprecated: use github.com/libp2p/go-libp2p-core/discovery.TTL instead.
func TTL(ttl time.Duration) moved.Option {
	return moved.TTL(ttl)
}

// Deprecated: use github.com/libp2p/go-libp2p-core/discovery.Limit instead.
func Limit(limit int) moved.Option {
	return moved.Limit(limit)
}
