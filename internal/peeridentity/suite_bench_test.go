package peeridentity

import (
	"encoding/binary"
	"fmt"
	"testing"
)

func BenchmarkPayloadSeal(b *testing.B) {
	payload := make([]byte, 4096)
	for level := 0; level <= 10; level++ {
		level := level
		b.Run(fmt.Sprintf("level-%02d", level), func(b *testing.B) {
			if level == 0 {
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_ = append([]byte(nil), payload...)
				}
				return
			}
			client, err := BeginInitiator(level, "bench-client", "client")
			if err != nil {
				b.Fatal(err)
			}
			server, err := BeginResponder(level, "bench-server", "server", client.Material())
			if err != nil {
				b.Fatal(err)
			}
			clientKeys, err := client.Finish(server.Material())
			if err != nil {
				b.Fatal(err)
			}
			serverKeys, err := server.Finish(client.Material())
			if err != nil {
				b.Fatal(err)
			}
			nonce := make([]byte, clientKeys.Send.NonceSize())
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				binary.BigEndian.PutUint64(nonce[len(nonce)-8:], uint64(i+1))
				sealed := clientKeys.Send.Seal(nil, nonce, payload, nil)
				if _, err := serverKeys.Recv.Open(nil, nonce, sealed, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
