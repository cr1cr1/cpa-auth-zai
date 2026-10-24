package main

import (
	"strings"
	"testing"
)

// Acceptance: upstream errors never return a supplied credential, even when
// the provider echoes it at the start or end of a structured or plain message.
func TestUpstreamErrorDetailRedactsSuppliedSecrets(t *testing.T) {
	secrets := []string{"tok-oauth", "tok-biz"}
	for _, body := range []string{
		`{"code":401,"msg":"tok-oauth expired; Bearer tok-biz"}`,
		`tok-oauth expired; Bearer tok-biz`,
	} {
		detail := upstreamErrorDetail([]byte(body), secrets...)
		if strings.Contains(detail, "tok-oauth") || strings.Contains(detail, "tok-biz") {
			t.Fatalf("detail leaked a credential: %q", detail)
		}
		if !strings.Contains(detail, "[redacted]") {
			t.Fatalf("detail must show that redaction occurred: %q", detail)
		}
	}
}
