package main

import (
	"testing"

	"graal-rc/internal/connection"
	"graal-rc/internal/credentials"
)

func TestEveryLoginUsesTheAlpaxisListserver(t *testing.T) {
	logins := map[string]connection.Credentials{
		"new account":   toCreds(LoginRequest{Account: "staff", Password: "secret"}),
		"saved account": accountToCreds(credentials.Account{Account: "staff", Password: "secret"}),
	}

	for name, got := range logins {
		if got.Host != "listserver.alpaxis.net" || got.Port != 14922 {
			t.Fatalf("%s: listserver = %s:%d, want listserver.alpaxis.net:14922", name, got.Host, got.Port)
		}
		if got.Account != "staff" || got.Password != "secret" {
			t.Fatalf("%s: credentials were not carried over: %+v", name, got)
		}
	}
}
