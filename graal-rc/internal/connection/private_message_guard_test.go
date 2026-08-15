package connection

import "testing"

func TestValidatePrivateMessage(t *testing.T) {
	for _, message := range []string{
		"Oi <3",
		"2 < 3",
		"Linha 1\nLinha 2",
	} {
		if err := validatePrivateMessage(message); err != nil {
			t.Fatalf("safe message %q was rejected: %v", message, err)
		}
	}

	for _, message := range []string{
		"<script>alert(1)</script>",
		`<img src=x onerror="alert(1)">`,
		`javascript:alert(1)`,
		`<iframe src="https://example.test"></iframe>`,
		`<div srcdoc="<script>alert(1)</script>">`,
	} {
		if err := validatePrivateMessage(message); err == nil {
			t.Fatalf("unsafe message %q was accepted", message)
		}
	}

	if err := validatePrivateMessage("   "); err == nil {
		t.Fatal("blank message was accepted")
	}
}
