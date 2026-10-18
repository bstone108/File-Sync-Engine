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

func (d PeerAPIDirectory) Publish(context.Context, string, []byte) error {
	return errors.New("connected peers do not store a new pairing record")
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
