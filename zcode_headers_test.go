package main

import (
	"regexp"
	"testing"
)

var zcodeUUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestZCodeFingerprintSessionAndRequestIDs(t *testing.T) {
	first := map[string][]string{}
	second := map[string][]string{}
	if errFingerprint := addZCodeFingerprint(first, "openai"); errFingerprint != nil {
		t.Fatalf("first fingerprint failed: %v", errFingerprint)
	}
	if errFingerprint := addZCodeFingerprint(second, "openai"); errFingerprint != nil {
		t.Fatalf("second fingerprint failed: %v", errFingerprint)
	}
	if first["X-Session-Id"][0] != second["X-Session-Id"][0] {
		t.Fatalf("session ID changed: %q != %q", first["X-Session-Id"], second["X-Session-Id"])
	}
	for _, name := range []string{"X-Query-Id", "X-Request-Id", "X-ZCode-Trace-Id"} {
		if first[name][0] == second[name][0] {
			t.Errorf("%s was reused across requests: %q", name, first[name])
		}
	}
}

func assertZCodeFingerprint(t *testing.T, headers map[string][]string, sdkVersion string) {
	t.Helper()
	want := map[string]string{
		"Accept":               "application/json, text/event-stream",
		"User-Agent":           "ZCode/3.10.2 ai-sdk/provider-utils/" + sdkVersion + " runtime/node.js/24",
		"HTTP-Referer":         "https://zcode.z.ai",
		"X-Title":              "Z Code@electron",
		"X-ZCode-App-Version":  "3.10.2",
		"X-ZCode-Agent":        "glm",
		"X-ZCode-Session-Type": "main",
		"X-Client-Language":    "en-US",
		"X-Release-Channel":    "production",
	}
	for name, value := range want {
		if got := headers[name]; len(got) != 1 || got[0] != value {
			t.Errorf("%s = %v, want %q", name, got, value)
		}
	}
	for _, name := range []string{"X-Platform", "X-Os-Category", "X-Os-Version", "X-Client-Timezone"} {
		if got := headers[name]; len(got) != 1 || got[0] == "" {
			t.Errorf("%s = %v, want one non-empty value", name, got)
		}
	}
	for _, name := range []string{"X-Session-Id", "X-Query-Id", "X-Request-Id", "X-ZCode-Trace-Id"} {
		if got := headers[name]; len(got) != 1 || !zcodeUUIDPattern.MatchString(got[0]) {
			t.Errorf("%s = %v, want one UUID v4", name, got)
		}
	}
}
