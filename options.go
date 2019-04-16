package discovery

import (
	"time"

	moved "github.com/libp2p/go-libp2p-core/discovery"
)

// Deprecated: use github.com/libp2p/go-libp2p-core/discovery.DiscoveryOpt instead.
type Option = moved.DiscoveryOpt

// Deprecated: use github.com/libp2p/go-libp2p-core/discovery.DiscoveryOpts instead.
type Options = moved.DiscoveryOpts

// TTL is an option that provides a hint for the duration of an advertisement
func TTL(ttl time.Duration) moved.DiscoveryOpt {
	return func(opts *moved.DiscoveryOpts) error {
		opts.Ttl = ttl
		return nil
	}
}

// Limit is an option that provides an upper bound on the peer count for discovery
func Limit(limit int) moved.DiscoveryOpt {
	return func(opts *moved.DiscoveryOpts) error {
		opts.Limit = limit
		return nil
	}
}
