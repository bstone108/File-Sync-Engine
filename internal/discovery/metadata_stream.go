package discovery

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"

	"filesyncengine/internal/structuredlog"
)

// MetadataProtocol carries the streamsync metadata and block session.
// It is the path used when a peer has no separate TCP stream endpoint.
const MetadataProtocol = "/fse/metadata/1.0.0"

type metadataServerFunc func(context.Context, io.ReadWriteCloser) error

var (
	metadataMu     sync.RWMutex
	metadataServer metadataServerFunc
)

// SetMetadataServer installs the handler for inbound metadata sessions.
func SetMetadataServer(fn metadataServerFunc) {
	metadataMu.Lock()
	metadataServer = fn
	metadataMu.Unlock()
}

func registerMetadataStream(h host.Host) {
	if h == nil {
		return
	}
	h.SetStreamHandler(protocol.ID(MetadataProtocol), func(stream network.Stream) {
		if stream == nil {
			return
		}
		defer stream.Close()
		metadataMu.RLock()
		fn := metadataServer
		metadataMu.RUnlock()
		if fn == nil {
			_ = stream.Reset()
			return
		}
		if err := fn(context.Background(), stream); err != nil {
			structuredlog.Event("warn", "metadata.stream.failed", "metadata session failed", map[string]any{
				"peer":  stream.Conn().RemotePeer().String(),
				"error": err.Error(),
			})
		}
	})
}

// DialMetadata opens a metadata session to a libp2p peer.
func DialMetadata(ctx context.Context, peerID string) (io.ReadWriteCloser, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	h := currentAPIHost()
	if h == nil {
		return nil, errNoAPIHost
	}
	pid, err := peer.Decode(peerID)
	if err != nil {
		return nil, err
	}
	if h.Network().Connectedness(pid) != network.Connected {
		return nil, errNoAPIHost
	}
	dialCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	stream, err := h.NewStream(dialCtx, pid, protocol.ID(MetadataProtocol))
	if err != nil {
		structuredlog.Event("warn", "metadata.stream.dial_failed", "metadata session was not opened", map[string]any{
			"peer":  peerID,
			"error": err.Error(),
		})
		return nil, err
	}
	return stream, nil
}
