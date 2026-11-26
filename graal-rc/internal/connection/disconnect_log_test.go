package connection

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"
)

// Without the reason, a dropped session cannot be diagnosed from the log.
func TestDisconnectIsLoggedWithItsReason(t *testing.T) {
	var out bytes.Buffer
	log.SetOutput(&out)
	defer log.SetOutput(os.Stderr)

	s := NewService()
	s.handle = 7
	if !s.disconnectSession(7, s.serverEpoch, "Connection closed") {
		t.Fatal("the current session was not disconnected")
	}
	if !strings.Contains(out.String(), `[connection] disconnected reason="Connection closed"`) {
		t.Fatalf("log = %q", out.String())
	}
}
