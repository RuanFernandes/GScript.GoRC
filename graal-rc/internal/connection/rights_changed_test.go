package connection

import "testing"

// Alpaxis announces a rights change as "has set the rights of". Missing it kept
// the script permissions loaded at login.
func TestAlpaxisRightsChangeNoticeIsRecognized(t *testing.T) {
	for message, want := range map[string]string{
		"[22:50] [RC] Nullborne has set the rights of Nullborne": "Nullborne",
		"Nullborne has set the rights of Nullborne":              "Nullborne",
		"Admin HAS SET THE RIGHTS OF (offline) player Test":      "Test",
		"[08:51] ruanf has set rights of (offline) player ruanf": "ruanf",
		"Nullborne has set the flags of Nullborne":               "",
		"Nullborne has set the rights of ":                       "",
	} {
		if got := rightsChangedTarget(message); got != want {
			t.Errorf("%q: target = %q, want %q", message, got, want)
		}
	}
}
