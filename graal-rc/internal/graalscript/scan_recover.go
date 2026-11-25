package graalscript

import "log"

// parseForScan keeps one script that crashes the parser from ending the app: the
// workspace scan runs in goroutines nothing else recovers.
func parseForScan(path string, parse func() *Document) (doc *Document, ok bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("[graalscript] skipped %s: parser panic: %v", path, recovered)
			doc, ok = nil, false
		}
	}()
	return parse(), true
}
