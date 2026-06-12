package daemonstop

import (
	"context"
	"testing"
	"time"
)

func TestSignalHandlerClosesDoneOnce(t *testing.T) {
	signal := NewSignal()

	if err := signal.Handler(context.Background()); err != nil {
		t.Fatalf("first handler call returned error: %v", err)
	}
	select {
	case <-signal.Done():
	case <-time.After(time.Second):
		t.Fatal("stop signal did not close after handler call")
	}

	if err := signal.Handler(context.Background()); err != nil {
		t.Fatalf("second handler call returned error: %v", err)
	}
	select {
	case <-signal.Done():
	case <-time.After(time.Second):
		t.Fatal("stop signal was not still closed after second handler call")
	}
}
