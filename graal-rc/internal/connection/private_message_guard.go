package connection

import (
	"errors"
	"regexp"
	"strings"
)

var unsafePrivateMessagePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?is)<\s*/?\s*(?:script|iframe|object|embed|svg|math|style|link|base|meta|form|input|img|video|audio|source|template)\b`),
	regexp.MustCompile(`(?is)(?:java|vb)script\s*:`),
	regexp.MustCompile(`(?is)(?:^|[\s"'])on[a-z][a-z0-9_-]*\s*=`),
	regexp.MustCompile(`(?is)\bsrcdoc\s*=`),
	regexp.MustCompile(`(?is)\bdata\s*:\s*text/html`),
}

func validatePrivateMessage(message string) error {
	if strings.TrimSpace(message) == "" {
		return errors.New("private message cannot be empty")
	}
	for _, pattern := range unsafePrivateMessagePatterns {
		if pattern.MatchString(message) {
			return errors.New("private message contains blocked HTML or script markup")
		}
	}
	return nil
}
