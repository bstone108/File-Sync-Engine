package link

import (
	"context"
	"errors"

	"filesyncengine/internal/discovery"
)

// PeerAPIDirectory asks engines that are already connected for a pairing
// record. It does not publish records; local discovery and the DHT do that.
type PeerAPIDirectory struct {
	Endpoints func() []string
}

// ErrPeerDirectoryLookupOnly means this directory answers lookups and does not
// publish. MultiDirectory treats it as expected, not as a rejected record.
var ErrPeerDirectoryLookupOnly = errors.New("connected peers look pairing records up and do not publish them")

func (d PeerAPIDirectory) Publish(context.Context, string, []byte) error {
	return ErrPeerDirectoryLookupOnly
}

func (d PeerAPIDirectory) Unpublish(context.Context, string) error {
	return nil
}

func (d PeerAPIDirectory) Lookup(ctx context.Context, namespace string) ([][]byte, error) {
	if d.Endpoints == nil {
		return nil, nil
	}
	var last error
	for _, endpoint := range d.Endpoints() {
		if endpoint == "" {
			continue
		}
		payload, err := discovery.FetchPairingRecord(ctx, endpoint, namespace)
		if err != nil {
			last = err
			continue
		}
		if len(payload) > 0 {
			return [][]byte{payload}, nil
		}
	}
	if last != nil && ctx != nil && ctx.Err() != nil {
		return nil, last
	}
	return nil, nil
}
