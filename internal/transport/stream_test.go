package transport

import (
	"bytes"
	"io"
	"testing"
)

func TestPipeTransportUsesExistingReadWriterWithoutDialing(t *testing.T) {
	input := bytes.NewBufferString("hello")
	output := &bytes.Buffer{}
	stream := NewPipeStream(input, output)

	buf := make([]byte, 5)
	if _, err := io.ReadFull(stream, buf); err != nil {
		t.Fatalf("read pipe stream: %v", err)
	}
	if string(buf) != "hello" {
		t.Fatalf("read %q", string(buf))
	}
	if _, err := stream.Write([]byte("world")); err != nil {
		t.Fatalf("write pipe stream: %v", err)
	}
	if output.String() != "world" {
		t.Fatalf("output = %q", output.String())
	}
}

func TestEndpointKindsSupportManualRelayProxyVPNAndPipe(t *testing.T) {
	for _, kind := range []EndpointKind{EndpointManual, EndpointRelay, EndpointProxy, EndpointVPN, EndpointPipe} {
		if !kind.Valid() {
			t.Fatalf("kind %s should be valid", kind)
		}
	}
	if EndpointKind("bogus").Valid() {
		t.Fatalf("bogus endpoint kind should not be valid")
	}
}
