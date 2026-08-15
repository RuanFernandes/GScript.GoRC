package main

import "testing"

func TestValidateAppThemeAcceptsSupportedColorValues(t *testing.T) {
	theme := AppTheme{
		Key:    "custom-ocean",
		Name:   "Ocean",
		Mode:   "dark",
		Colors: map[string]string{"background": "#102030", "windowBorder": "color-mix(in srgb, #102030 70%, #5dd6c0)"},
	}
	if err := validateAppTheme(theme); err != nil {
		t.Fatalf("validate app theme: %v", err)
	}
}

func TestValidateAppThemeRejectsUnsafeOrUnknownValues(t *testing.T) {
	cases := []AppTheme{
		{Key: "Custom", Name: "Unsafe key", Mode: "dark", Colors: map[string]string{"background": "#000000"}},
		{Key: "custom-url", Name: "URL", Mode: "dark", Colors: map[string]string{"background": "url(https://example.test/theme)"}},
		{Key: "custom-css", Name: "CSS", Mode: "dark", Colors: map[string]string{"background": "#000000; color: red"}},
		{Key: "custom-unknown", Name: "Unknown", Mode: "dark", Colors: map[string]string{"notAColor": "#000000"}},
	}
	for _, theme := range cases {
		if err := validateAppTheme(theme); err == nil {
			t.Fatalf("validateAppTheme(%q) accepted invalid input", theme.Key)
		}
	}
}
