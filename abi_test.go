package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
)

// Acceptance: token-only inference is blocked upstream, so registration must
// not advertise model or executor capabilities. Schema version 6 keeps
// management JSON unescaped.
func TestRegistrationBlocksTokenOnlyInference(t *testing.T) {
	raw, errHandle := handleCall("plugin.register", []byte("{}"))
	if errHandle != nil {
		t.Fatalf("plugin.register failed: %v", errHandle)
	}
	var envelope pluginabi.Envelope
	if errUnmarshal := json.Unmarshal(raw, &envelope); errUnmarshal != nil || !envelope.OK {
		t.Fatalf("registration envelope invalid: ok=%v err=%v", envelope.OK, errUnmarshal)
	}
	var registration struct {
		SchemaVersion uint32 `json:"schema_version"`
		Metadata      struct {
			Name    string `json:"Name"`
			Version string `json:"Version"`
		} `json:"metadata"`
		Capabilities map[string]any `json:"capabilities"`
	}
	if errUnmarshal := json.Unmarshal(envelope.Result, &registration); errUnmarshal != nil {
		t.Fatalf("decode registration: %v", errUnmarshal)
	}
	if registration.SchemaVersion != 6 {
		t.Fatalf("schema_version = %d, want 6", registration.SchemaVersion)
	}
	if registration.Metadata.Name != pluginID || registration.Metadata.Version == "" {
		t.Fatalf("metadata = %+v", registration.Metadata)
	}
	for _, capability := range []string{"auth_provider", "quota_provider", "management_api"} {
		if registration.Capabilities[capability] != true {
			t.Fatalf("capability %s not declared: %v", capability, registration.Capabilities)
		}
	}
	if _, advertised := registration.Capabilities["model_provider"]; advertised {
		t.Fatalf("model_provider advertised while token-only inference is blocked: %v", registration.Capabilities)
	}
	for capability := range registration.Capabilities {
		if capability == "executor" || strings.HasPrefix(capability, "executor_") {
			t.Fatalf("executor capability %q advertised while token-only inference is blocked: %v", capability, registration.Capabilities)
		}
	}
}

// Acceptance: an unknown method must fail with an explicit error envelope, not
// a silent success.
func TestUnknownMethodFails(t *testing.T) {
	raw, errHandle := handleCall("totally.unknown", []byte("{}"))
	if errHandle == nil {
		t.Fatalf("unknown method succeeded: %s", raw)
	}
	if !bytes.Contains(raw, []byte("unknown_method")) && !strings.Contains(errHandle.Error(), "unknown method") {
		t.Fatalf("error should mention the unknown method, got %q / %s", errHandle.Error(), raw)
	}
}

// Acceptance: error envelopes carry the upstream HTTP status so CPA maps 401,
// 403, and 429 into the documented client errors.
func TestErrorEnvelopeCarriesHTTPStatus(t *testing.T) {
	raw := errEnvelope("insufficient_quota", "plan limit reached", 403)
	var envelope struct {
		OK    bool `json:"ok"`
		Error *struct {
			Code       string `json:"code"`
			Message    string `json:"message"`
			HTTPStatus int    `json:"http_status"`
		} `json:"error"`
	}
	if errUnmarshal := json.Unmarshal(raw, &envelope); errUnmarshal != nil {
		t.Fatalf("decode envelope: %v", errUnmarshal)
	}
	if envelope.OK || envelope.Error == nil || envelope.Error.HTTPStatus != 403 {
		t.Fatalf("envelope = %s", raw)
	}
}

// Acceptance: stored credentials are typed by purpose and never substituted
// with API keys. A record without login tokens is rejected.
func TestParseZaiStorageRequiresLoginToken(t *testing.T) {
	valid, errParse := parseZaiStorage([]byte(`{"version":1,"type":"zai","oauth_access_token":"tok","email":"a@b.c"}`))
	if errParse != nil {
		t.Fatalf("valid storage rejected: %v", errParse)
	}
	if valid.authID() != "zai-a-b-c" {
		t.Fatalf("authID = %q", valid.authID())
	}
	candidates := valid.bearerCandidates()
	if len(candidates) != 1 || candidates[0] != "tok" {
		t.Fatalf("bearerCandidates = %v", candidates)
	}
	if _, errParse := parseZaiStorage([]byte(`{"version":1,"type":"zai"}`)); errParse == nil {
		t.Fatalf("storage without tokens must be rejected")
	}
	if _, errParse := parseZaiStorage([]byte(`{"version":1,"type":"other","api_key":"k"}`)); errParse == nil {
		t.Fatalf("foreign storage type must be rejected")
	}
}

// Acceptance: upstream error details are trimmed and capped so bodies cannot
// flood client responses or leak header material.
func TestUpstreamErrorDetailIsSanitized(t *testing.T) {
	long := strings.Repeat("x", 500)
	if got := upstreamErrorDetail([]byte(`{"code":1113,"msg":"` + long + `","success":false}`)); len(got) > 300 {
		t.Fatalf("detail not capped: %d", len(got))
	}
	if got := upstreamErrorDetail([]byte("not json")); !strings.HasPrefix(got, "not json") {
		t.Fatalf("plain body detail = %q", got)
	}
}
