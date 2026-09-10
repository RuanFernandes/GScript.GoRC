package connection

import "testing"

func TestParseSelfChatIdentity(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		kind  string
		actor string
		value string
	}{
		{
			name:  "account response",
			text:  "Zodiac (Server): Graal745039's Account Name is Graal745039",
			kind:  "account",
			actor: "Graal745039",
			value: "Graal745039",
		},
		{
			name:  "community response with display prefix",
			text:  "[13:31] [RC] Zodiac (Server): ruanf's Community Name is ruanf",
			kind:  "community",
			actor: "ruanf",
			value: "ruanf",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := parseSelfChatIdentity(test.text)
			if !ok {
				t.Fatalf("parseSelfChatIdentity(%q) returned false", test.text)
			}
			if got.kind != test.kind || got.actor != test.actor || got.value != test.value {
				t.Fatalf("parseSelfChatIdentity(%q) = %+v, want kind=%q actor=%q value=%q", test.text, got, test.kind, test.actor, test.value)
			}
		})
	}
}

func TestCaptureSelfChatIdentityOnlyForOwnAlias(t *testing.T) {
	service := &Service{creds: Credentials{Account: "ruanf"}}
	service.handleRCMessage("Zodiac (Server): another's Community Name is OtherCommunity")
	if service.selfRightsCommunityName != "" {
		t.Fatalf("other player's identity was captured: %q", service.selfRightsCommunityName)
	}

	service.handleRCMessage("Zodiac (Server): ruanf's Community Name is RuanCommunity")
	if service.selfRightsCommunityName != "RuanCommunity" {
		t.Fatalf("self community = %q, want %q", service.selfRightsCommunityName, "RuanCommunity")
	}
}
