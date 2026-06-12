package transport

import "io"

type Stream interface {
	io.Reader
	io.Writer
}

type PipeStream struct {
	reader io.Reader
	writer io.Writer
}

func NewPipeStream(reader io.Reader, writer io.Writer) PipeStream {
	return PipeStream{reader: reader, writer: writer}
}

func (s PipeStream) Read(p []byte) (int, error) {
	return s.reader.Read(p)
}

func (s PipeStream) Write(p []byte) (int, error) {
	return s.writer.Write(p)
}

type EndpointKind string

const (
	EndpointManual EndpointKind = "manual"
	EndpointRelay  EndpointKind = "relay"
	EndpointProxy  EndpointKind = "proxy"
	EndpointVPN    EndpointKind = "vpn"
	EndpointPipe   EndpointKind = "pipe"
)

func (k EndpointKind) Valid() bool {
	switch k {
	case EndpointManual, EndpointRelay, EndpointProxy, EndpointVPN, EndpointPipe:
		return true
	default:
		return false
	}
}
