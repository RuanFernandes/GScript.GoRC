package connection

import "testing"

func TestFindExplicitMention(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		aliases []string
		want    string
	}{
		{name: "account at start", text: "@GraalAccount hello", aliases: []string{"GraalAccount"}, want: "GraalAccount"},
		{name: "community case insensitive", text: "oi, @ruanf!", aliases: []string{"RuanF"}, want: "RuanF"},
		{name: "punctuation boundary", text: "(@CommunityName)", aliases: []string{"CommunityName"}, want: "CommunityName"},
		{name: "longest identity wins", text: "@foo-bar", aliases: []string{"foo", "foo-bar"}, want: "foo-bar"},
		{name: "missing at", text: "GraalAccount hello", aliases: []string{"GraalAccount"}},
		{name: "longer account does not match", text: "@GraalAccountExtra", aliases: []string{"GraalAccount"}},
		{name: "email suffix does not match", text: "mail me at user@GraalAccount", aliases: []string{"GraalAccount"}},
		{name: "double at does not match", text: "@@GraalAccount", aliases: []string{"GraalAccount"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := findExplicitMention(test.text, test.aliases); got != test.want {
				t.Fatalf("findExplicitMention(%q, %v) = %q, want %q", test.text, test.aliases, got, test.want)
			}
		})
	}
}

func TestServiceSelfMentionUsesAccountIdentitiesOnly(t *testing.T) {
	service := &Service{
		creds:                   Credentials{Account: "LoginAccount", Nickname: "Ruan"},
		selfRightsAccount:       "CanonicalAccount",
		selfRightsCommunityName: "CommunityName",
	}

	for _, test := range []struct {
		text string
		want bool
	}{
		{text: "Zodiac: @LoginAccount hello", want: true},
		{text: "Zodiac: @CanonicalAccount hello", want: true},
		{text: "Zodiac: @CommunityName hello", want: true},
		{text: "Zodiac: @Ruan hello", want: false},
	} {
		if got := service.IsSelfMention(test.text); got != test.want {
			t.Errorf("IsSelfMention(%q) = %v, want %v", test.text, got, test.want)
		}
	}
}

func TestServiceSelfMentionUsesPlayerPropertyCommunity(t *testing.T) {
	service := &Service{
		creds:             Credentials{Account: "CanonicalAccount"},
		selfPlayerID:      42,
		playerAccounts:    map[int]string{42: "CanonicalAccount"},
		playerCommunities: map[int]string{42: "PlayerCommunity"},
	}

	if got := service.SelfMentionTarget("[13:31] [RC] Zodiac (Server): hey @PlayerCommunity, olha"); got != "PlayerCommunity" {
		t.Fatalf("SelfMentionTarget() = %q, want %q", got, "PlayerCommunity")
	}
}

func TestServiceSelfMentionHandlesRCChatPrefixes(t *testing.T) {
	service := &Service{
		creds:                   Credentials{Account: "Graal5766947"},
		selfRightsCommunityName: "Repinho",
	}

	for _, text := range []string{
		"Zodiac (Server): @Graal5766947 oi",
		"[13:31] [RC] Zodiac (Server): oi @Repinho!",
		"<> Zodiac: fala @repinho?",
	} {
		if !service.IsSelfMention(text) {
			t.Fatalf("IsSelfMention(%q) = false, want true", text)
		}
	}
}
