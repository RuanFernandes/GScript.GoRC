package main

import (
	"context"
	"testing"
	"time"
)

func TestReconnectDelayUsesBoundedBackoff(t *testing.T) {
	if got := reconnectDelay(1); got != 0 {
		t.Fatalf("first attempt delay = %s, want immediate", got)
	}
	if got := reconnectDelay(2); got != time.Second {
		t.Fatalf("second attempt delay = %s, want 1s", got)
	}
	if got := reconnectDelay(5); got != 8*time.Second {
		t.Fatalf("fifth attempt delay = %s, want 8s", got)
	}
	if got := reconnectDelay(reconnectMaxAttempts); got != reconnectMaxDelay {
		t.Fatalf("maximum delay = %s, want %s", got, reconnectMaxDelay)
	}
}

func TestWaitReconnectCanBeCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if waitReconnect(ctx, time.Hour) {
		t.Fatal("cancelled recovery wait returned success")
	}
}
