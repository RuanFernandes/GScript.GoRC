package sync

import (
	"crypto/md5"
	"encoding/hex"
	"strings"
)

// normalizeEOL converts CRLF (and a stray CR) to LF. The Windows DLL may emit
// CRLF while editors save LF; without normalization the two read as different
// and trigger phantom conflicts.
func normalizeEOL(s string) string {
	// § is the wire representation used by the NC protocol for line breaks.
	s = strings.ReplaceAll(s, "\xA7", "\n")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

// HashScript returns a short hex digest of the normalized script content.
// Short (16 chars) is plenty — this only needs to detect changes, not resist
// collision attacks, and a short digest keeps the manifest compact.
func HashScript(content string) string {
	sum := md5.Sum([]byte(normalizeEOL(content)))
	return hex.EncodeToString(sum[:])
}
