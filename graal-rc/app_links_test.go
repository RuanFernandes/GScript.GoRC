package main

import (
	"net/url"
	"strings"
	"testing"
)

func TestChatLinkRouteURLPreservesURL(t *testing.T) {
	link := "https://gscript.dev/#beautify?value=one&other=two"
	route := chatLinkRouteURL(link)
	const prefix = "/#chat-link?u="
	encoded := strings.TrimPrefix(route, prefix)
	if encoded == route {
		t.Fatalf("chatLinkRouteURL(%q) produced an unexpected route %q", link, route)
	}
	got, err := url.QueryUnescape(encoded)
	if err != nil {
		t.Fatalf("chatLinkRouteURL(%q) produced invalid URL encoding: %v", link, err)
	}
	if got != link {
		t.Fatalf("chatLinkRouteURL(%q) encoded URL as %q", link, got)
	}
}

func TestSanitizeChatLinkURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "https with fragment", input: " https://gscript.dev/#beautify ", want: "https://gscript.dev/#beautify"},
		{name: "http with query", input: "http://example.test/docs?q=1", want: "http://example.test/docs?q=1"},
		{name: "javascript scheme", input: "javascript:alert(1)", wantErr: true},
		{name: "local file", input: "file:///C:/secret.txt", wantErr: true},
		{name: "missing host", input: "https:///missing-host", wantErr: true},
		{name: "unsupported scheme", input: "ftp://example.test/file.txt", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := sanitizeChatLinkURL(test.input)
			if test.wantErr {
				if err == nil {
					t.Fatalf("sanitizeChatLinkURL(%q) expected an error", test.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("sanitizeChatLinkURL(%q) returned error: %v", test.input, err)
			}
			if got != test.want {
				t.Fatalf("sanitizeChatLinkURL(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}
