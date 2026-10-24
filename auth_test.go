package main

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

const (
	authTestFlowID  = "flow-123"
	authTestState   = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	authTestAuthURL = "https://zcode.z.ai/oauth/authorize?flow=flow-123&state=" + authTestState
	authTestEmail   = "user@example.com"
)

// authHostCall records one scripted upstream request.
type authHostCall struct {
	Method  string
	URL     string
	Headers map[string][]string
	Body    []byte
}

// authFakeHost replaces hostCaller and records every host.http.do request.
type authFakeHost struct {
	mu      sync.Mutex
	calls   []authHostCall
	logs    []capturedHostLog
	respond func(authHostCall) []byte
}

func (f *authFakeHost) recorded() []authHostCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]authHostCall(nil), f.calls...)
}

func (f *authFakeHost) recordedLogs() []capturedHostLog {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]capturedHostLog(nil), f.logs...)
}

func (f *authFakeHost) countURL(prefix string) int {
	count := 0
	for _, call := range f.recorded() {
		if strings.HasPrefix(call.URL, prefix) {
			count++
		}
	}
	return count
}

// installAuthFakeHost swaps hostCaller for the scripted fake and restores the
// previous caller when the test ends.
func installAuthFakeHost(t *testing.T, respond func(authHostCall) []byte) *authFakeHost {
	t.Helper()
	fake := &authFakeHost{respond: respond}
	prev := hostCaller
	hostCaller = func(method string, payload []byte) ([]byte, error) {
		if method == pluginabi.MethodHostLog {
			var entry capturedHostLog
			if errUnmarshal := json.Unmarshal(payload, &entry); errUnmarshal != nil {
				return nil, errUnmarshal
			}
			fake.mu.Lock()
			fake.logs = append(fake.logs, entry)
			fake.mu.Unlock()
			return hostEnvelope(t, struct{}{}), nil
		}
		var req hostHTTPRequest
		if errUnmarshal := json.Unmarshal(payload, &req); errUnmarshal != nil {
			return nil, errUnmarshal
		}
		call := authHostCall{Method: req.Request.Method, URL: req.Request.URL, Headers: req.Request.Headers, Body: req.Request.Body}
		fake.mu.Lock()
		fake.calls = append(fake.calls, call)
		fake.mu.Unlock()
		return fake.respond(call), nil
	}
	t.Cleanup(func() { hostCaller = prev })
	return fake
}

// authHostEnvelope scripts one upstream HTTP response inside a host envelope.
func authHostEnvelope(status int, body string) []byte {
	resultRaw, _ := json.Marshal(hostHTTPResponse{StatusCode: status, Body: []byte(body)})
	envelopeRaw, _ := json.Marshal(pluginabi.Envelope{OK: true, Result: resultRaw})
	return envelopeRaw
}

func callAuthMethod(t *testing.T, method, payload string) (json.RawMessage, error) {
	t.Helper()
	raw, errHandle := handleCall(method, []byte(payload))
	if errHandle != nil {
		return nil, errHandle
	}
	var envelope pluginabi.Envelope
	if errUnmarshal := json.Unmarshal(raw, &envelope); errUnmarshal != nil {
		t.Fatalf("decode envelope for %s: %v", method, errUnmarshal)
	}
	if !envelope.OK {
		t.Fatalf("envelope for %s not ok", method)
	}
	return envelope.Result, nil
}

func decodeAuthResult(t *testing.T, raw json.RawMessage, out any) {
	t.Helper()
	if errUnmarshal := json.Unmarshal(raw, out); errUnmarshal != nil {
		t.Fatalf("decode %T: %v", out, errUnmarshal)
	}
}

func resetAuthFlows() {
	flowsMu.Lock()
	flows = map[string]*loginFlow{}
	flowsMu.Unlock()
}

func authInitBody(expiresAt any, pollIntervalSec int) string {
	data := map[string]any{"flow_id": authTestFlowID, "authorize_url": authTestAuthURL}
	if expiresAt != nil {
		data["expires_at"] = expiresAt
	}
	if pollIntervalSec > 0 {
		data["poll_interval_sec"] = pollIntervalSec
	}
	raw, _ := json.Marshal(map[string]any{"code": 0, "msg": "ok", "data": data})
	return string(raw)
}

func authReadyBody() string {
	return `{"code":0,"data":{"status":"ready","token":"zcode-jwt","zai.access_token":"oauth-tok","user":{"email":"` + authTestEmail + `","id":"user-1"}}}`
}

// authLoginRoutes scripts the full happy-path upstream conversation.
func authLoginRoutes(t *testing.T, pollBody func() string) *authFakeHost {
	return installAuthFakeHost(t, func(call authHostCall) []byte {
		switch {
		case call.URL == cliInitURL:
			return authHostEnvelope(200, authInitBody("2026-10-23T19:00:00Z", 0))
		case strings.HasPrefix(call.URL, cliPollURL):
			return authHostEnvelope(200, pollBody())
		case call.URL == businessLoginURL:
			if string(call.Body) != `{"token":"oauth-tok"}` {
				t.Errorf("business login body = %s", call.Body)
			}
			return authHostEnvelope(200, `{"code":200,"data":{"access_token":"biz-jwt"}}`)
		case call.URL == customerInfoURL:
			return authHostEnvelope(200, `{"code":200,"data":{"organizations":[{"organizationId":"org-first","isDefault":false},{"organizationId":"org-default","isDefault":true,"projects":[{"projectId":"proj-default","id":"x"}]}]}}`)
		case call.URL == "https://api.z.ai/api/biz/v1/organization/org-default/projects/proj-default/api_keys":
			if call.Method != http.MethodGet || strings.Join(call.Headers["Authorization"], "") != "Bearer biz-jwt" {
				t.Errorf("key list request = %s %s %v", call.Method, call.URL, call.Headers["Authorization"])
			}
			return authHostEnvelope(200, `{"code":200,"data":[{"name":"another-key","apiKey":"other-id","secretKey":"other-secret"},{"name":"cpa-auth-zai","apiKey":"key-id","secretKey":"key-secret"}]}`)
		}
		t.Errorf("unexpected upstream call %s %s", call.Method, call.URL)
		return authHostEnvelope(404, `{"code":404,"msg":"not found"}`)
	})
}

func authStart(t *testing.T) pluginapi.AuthLoginStartResponse {
	t.Helper()
	raw, errHandle := callAuthMethod(t, pluginabi.MethodAuthLoginStart, `{"Provider":"zai","host_callback_id":"cb-1"}`)
	if errHandle != nil {
		t.Fatalf("auth.login.start failed: %v", errHandle)
	}
	var resp pluginapi.AuthLoginStartResponse
	decodeAuthResult(t, raw, &resp)
	return resp
}

func authPoll(t *testing.T, state string) pluginapi.AuthLoginPollResponse {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"Provider": "zai", "State": state, "host_callback_id": "cb-1"})
	raw, errHandle := callAuthMethod(t, pluginabi.MethodAuthLoginPoll, string(payload))
	if errHandle != nil {
		t.Fatalf("auth.login.poll failed: %v", errHandle)
	}
	var resp pluginapi.AuthLoginPollResponse
	decodeAuthResult(t, raw, &resp)
	return resp
}

func requireHexToken(t *testing.T, value string, length int) {
	t.Helper()
	if len(value) != length {
		t.Fatalf("token length = %d, want %d", len(value), length)
	}
	if _, errDecode := hex.DecodeString(value); errDecode != nil {
		t.Fatalf("token is not hex: %v", errDecode)
	}
}

// Acceptance: login start preserves the upstream authorize URL and shares its
// state with CPA while keeping the poll token out of the URL.

// lookupAuthFlow reads plugin-private login state for assertions; it lives in
// the test build only because production code must not depend on it.
func lookupAuthFlow(state string) *loginFlow {
	flowsMu.Lock()
	defer flowsMu.Unlock()
	return flows[state]
}

func TestLoginStartReturnsURLAndState(t *testing.T) {
	resetAuthFlows()
	t.Cleanup(resetAuthFlows)
	fake := installAuthFakeHost(t, func(call authHostCall) []byte {
		if call.URL != cliInitURL || call.Method != http.MethodPost {
			t.Errorf("init call = %s %s", call.Method, call.URL)
		}
		if string(call.Body) != `{"provider":"zai"}` {
			t.Errorf("init body = %s", call.Body)
		}
		auth := call.Headers["Authorization"]
		if len(auth) != 1 || !strings.HasPrefix(auth[0], "Bearer ") {
			t.Fatalf("init Authorization = %v", auth)
		}
		requireHexToken(t, strings.TrimPrefix(auth[0], "Bearer "), 64)
		return authHostEnvelope(200, authInitBody(time.Now().Add(4*time.Minute).UTC().Format(time.RFC3339), 2))
	})
	resp := authStart(t)
	if resp.Provider != providerID || resp.State != authTestState || resp.URL != authTestAuthURL {
		t.Fatalf("start response must preserve upstream URL and state: %+v", resp)
	}
	requireHexToken(t, resp.State, 64)
	authorizeURL, errParse := url.Parse(resp.URL)
	if errParse != nil {
		t.Fatalf("parse authorize URL: %v", errParse)
	}
	if got := authorizeURL.Query().Get("state"); got != resp.State {
		t.Fatalf("authorize state = %q, want CPA state %q", got, resp.State)
	}
	if got := authorizeURL.Query().Get("flow"); got != authTestFlowID {
		t.Fatalf("authorize flow = %q, want %q", got, authTestFlowID)
	}
	if resp.ExpiresAt.Before(time.Now()) || resp.ExpiresAt.After(time.Now().Add(defaultLoginTTL)) {
		t.Fatalf("ExpiresAt outside clamp window: %v", resp.ExpiresAt)
	}
	if _, present := resp.Metadata["flow_id"]; present {
		t.Fatalf("flow_id must not reach the browser: %v", resp.Metadata)
	}
	if _, present := resp.Metadata["poll_token"]; present {
		t.Fatalf("poll_token must not reach the browser: %v", resp.Metadata)
	}
	if strings.Contains(resp.URL, "poll") || !strings.HasPrefix(resp.URL, "https://") {
		t.Fatalf("authorize URL must be an https URL without credentials: %s", resp.URL)
	}
	if flow := lookupAuthFlow(resp.State); flow == nil || flow.pollInterval != 2*time.Second {
		t.Fatalf("flow not persisted with upstream interval: %+v", lookupAuthFlow(resp.State))
	}
	if fake.countURL(cliInitURL) != 1 {
		t.Fatalf("init calls = %d, want 1", fake.countURL(cliInitURL))
	}
	logs := fake.recordedLogs()
	if len(logs) != 1 || logs[0].Level != "info" || !strings.Contains(logs[0].Message, "OAuth login started") {
		t.Fatalf("login start logs = %+v", logs)
	}
}

// Acceptance: CPA and z.ai need a shared state, so a missing upstream state is
// an error rather than a generated replacement.
func TestLoginStartRequiresUpstreamState(t *testing.T) {
	resetAuthFlows()
	t.Cleanup(resetAuthFlows)
	installAuthFakeHost(t, func(call authHostCall) []byte {
		return authHostEnvelope(200, `{"code":0,"data":{"flow_id":"flow-123","authorize_url":"https://zcode.z.ai/oauth/authorize?flow=flow-123"}}`)
	})
	_, errHandle := handleCall(pluginabi.MethodAuthLoginStart, []byte(`{"Provider":"zai","host_callback_id":"cb-1"}`))
	if errHandle == nil || !strings.Contains(errHandle.Error(), "missing state") {
		t.Fatalf("login start error = %v, want missing state", errHandle)
	}
}

// Acceptance: upstream rejection surfaces a short sanitized message; a long
// hostile body must not leak past the detail cap.

// Acceptance: upstream expiry parsing handles RFC3339, unix seconds, unix
// milliseconds, and junk without ever producing a far-future or past flow.
func TestParseUpstreamExpiryVariants(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name   string
		value  any
		wantOK bool
		wantIn bool // inside a sane (now-1m, now+24h] window
	}{
		{"rfc3339", now.Add(3 * time.Minute).UTC().Format(time.RFC3339), true, true},
		{"unix-seconds", float64(now.Add(2 * time.Minute).Unix()), true, true},
		{"unix-ms-number", float64(now.Add(2 * time.Minute).UnixMilli()), true, false},
		{"unix-ms-string", strconv.FormatInt(now.Add(2*time.Minute).UnixMilli(), 10), true, false},
		{"junk", "not-a-time", false, false},
		{"empty", "", false, false},
	}
	for _, tc := range cases {
		parsed, ok := parseUpstreamExpiry(tc.value)
		if ok != tc.wantOK {
			t.Fatalf("%s: ok = %v, want %v", tc.name, ok, tc.wantOK)
		}
		if !tc.wantOK {
			continue
		}
		sane := parsed.After(now.Add(-time.Minute)) && parsed.Before(now.Add(24*time.Hour))
		if sane != tc.wantIn {
			t.Fatalf("%s: parsed %v sane=%v, want sane=%v", tc.name, parsed, sane, tc.wantIn)
		}
	}
}

func TestLoginStartUpstreamFailureIsSanitized(t *testing.T) {
	resetAuthFlows()
	t.Cleanup(resetAuthFlows)
	installAuthFakeHost(t, func(call authHostCall) []byte {
		return authHostEnvelope(401, `{"code":1113,"msg":"cli token rejected"}`)
	})
	if _, errHandle := handleCall(pluginabi.MethodAuthLoginStart, []byte(`{"Provider":"zai","host_callback_id":"cb-1"}`)); errHandle == nil {
		t.Fatalf("upstream 401 must fail login start")
	} else if !strings.Contains(errHandle.Error(), "401") || !strings.Contains(errHandle.Error(), "cli token rejected") {
		t.Fatalf("error = %q, want status and sanitized detail", errHandle.Error())
	}
	installAuthFakeHost(t, func(call authHostCall) []byte {
		return authHostEnvelope(500, strings.Repeat("x", 5000)+" SECRET-VALUE-XYZ")
	})
	_, errHandle := handleCall(pluginabi.MethodAuthLoginStart, []byte(`{"Provider":"zai","host_callback_id":"cb-1"}`))
	if errHandle == nil {
		t.Fatalf("upstream 500 must fail login start")
	}
	if strings.Contains(errHandle.Error(), "SECRET-VALUE-XYZ") {
		t.Fatalf("error leaked body tail: %q", errHandle.Error())
	}
	if len(errHandle.Error()) > 400 {
		t.Fatalf("error detail not capped: %d chars", len(errHandle.Error()))
	}
}

// Acceptance: a poll before completion reports pending and the recorded poll
// request carries the flow id and the bearer poll token.
func TestLoginPollPending(t *testing.T) {
	resetAuthFlows()
	t.Cleanup(resetAuthFlows)
	fake := authLoginRoutes(t, func() string { return `{"code":0,"data":{"status":"pending"}}` })
	started := authStart(t)
	resp := authPoll(t, started.State)
	if resp.Status != pluginapi.AuthLoginStatusPending {
		t.Fatalf("status = %q, want pending", resp.Status)
	}
	polls := fake.recorded()
	if fake.countURL(cliPollURL) != 1 {
		t.Fatalf("poll calls = %d, want 1", fake.countURL(cliPollURL))
	}
	last := polls[len(polls)-1]
	if last.URL != cliPollURL+authTestFlowID {
		t.Fatalf("poll URL = %s", last.URL)
	}
	flow := lookupAuthFlow(started.State)
	if flow == nil {
		t.Fatalf("flow missing for state %s", started.State)
	}
	if want := "Bearer " + flow.pollToken; strings.Join(last.Headers["Authorization"], ",") != want {
		t.Fatalf("poll Authorization mismatch: %v", last.Headers["Authorization"])
	}
	if want := "Bearer " + flow.pollToken; want == "Bearer " {
		t.Fatalf("flow poll token empty")
	}
	if logs := fake.recordedLogs(); len(logs) != 1 {
		t.Fatalf("pending poll must stay quiet; logs = %+v", logs)
	}
}

// Acceptance: an empty upstream failure retains the HTTP status needed to
// diagnose a rejected poll.
func TestLoginPollEmptyFailureIncludesStatus(t *testing.T) {
	resetAuthFlows()
	t.Cleanup(resetAuthFlows)
	fake := installAuthFakeHost(t, func(call authHostCall) []byte {
		if call.URL == cliInitURL {
			return authHostEnvelope(200, authInitBody(nil, 0))
		}
		return authHostEnvelope(http.StatusBadGateway, "")
	})
	started := authStart(t)
	resp := authPoll(t, started.State)
	if resp.Status != pluginapi.AuthLoginStatusError || !strings.Contains(resp.Message, "502") {
		t.Fatalf("poll = %+v, want error containing HTTP 502", resp)
	}
	logs := fake.recordedLogs()
	if len(logs) != 2 || logs[1].Level != "error" || !strings.Contains(logs[1].Fields["error"].(string), "502") {
		t.Fatalf("failed poll logs = %+v", logs)
	}
}

// Acceptance: a ready poll exchanges once, stores oauth+zcode+biz tokens with
// account identity, enriches org/project metadata, and consumes the flow.
func TestLoginPollSuccess(t *testing.T) {
	resetAuthFlows()
	t.Cleanup(resetAuthFlows)
	fake := authLoginRoutes(t, func() string { return authReadyBody() })
	started := authStart(t)
	resp := authPoll(t, started.State)
	if resp.Status != pluginapi.AuthLoginStatusSuccess {
		t.Fatalf("status = %q (%s), want success", resp.Status, resp.Message)
	}
	if resp.Auth.Provider != providerID {
		t.Fatalf("Auth.Provider = %q", resp.Auth.Provider)
	}
	if resp.Auth.Label != "Z.ai "+authTestEmail {
		t.Fatalf("Auth.Label = %q", resp.Auth.Label)
	}
	if resp.Auth.ID != "zai-user-example-com" || resp.Auth.FileName != "zai-user-example-com.json" {
		t.Fatalf("Auth.ID/FileName = %q / %q", resp.Auth.ID, resp.Auth.FileName)
	}
	var storage zaiStorage
	if errUnmarshal := json.Unmarshal(resp.Auth.StorageJSON, &storage); errUnmarshal != nil {
		t.Fatalf("decode StorageJSON: %v", errUnmarshal)
	}
	if storage.OAuthAccessToken != "oauth-tok" || storage.ZcodeJWT != "zcode-jwt" || storage.BizAccessToken != "biz-jwt" {
		t.Fatalf("storage tokens = %+v", storage)
	}
	var storedFields map[string]any
	if errUnmarshal := json.Unmarshal(resp.Auth.StorageJSON, &storedFields); errUnmarshal != nil {
		t.Fatalf("decode storage fields: %v", errUnmarshal)
	}
	if storedFields["api_key"] != "key-id.key-secret" {
		t.Fatalf("stored api_key = %v, want exact-name project key", storedFields["api_key"])
	}
	if storage.Email != authTestEmail || storage.Type != providerID || storage.Version != storageVersion {
		t.Fatalf("storage identity = %+v", storage)
	}
	if resp.Auth.Metadata["organization_id"] != "org-default" || resp.Auth.Metadata["project_id"] != "proj-default" {
		t.Fatalf("Auth.Metadata = %v", resp.Auth.Metadata)
	}
	if resp.Auth.Metadata["type"] != providerID {
		t.Fatalf("Auth.Metadata type = %v", resp.Auth.Metadata["type"])
	}
	if again := authPoll(t, started.State); again.Status != pluginapi.AuthLoginStatusError {
		t.Fatalf("replay poll status = %q, want error (flow must be consumed)", again.Status)
	}
	logs := fake.recordedLogs()
	if len(logs) != 3 || logs[1].Level != "info" || !strings.Contains(logs[1].Message, "OAuth login succeeded") {
		t.Fatalf("successful login logs = %+v", logs)
	}
}

func TestLoginStopsWhenAPIKeyProvisioningFails(t *testing.T) {
	resetAuthFlows()
	t.Cleanup(resetAuthFlows)
	fake := authLoginRoutes(t, func() string { return authReadyBody() })
	baseRespond := fake.respond
	fake.respond = func(call authHostCall) []byte {
		if strings.HasSuffix(call.URL, "/api_keys") {
			if call.Method != http.MethodGet {
				t.Errorf("list failure must not create a key: %s", call.Method)
			}
			return authHostEnvelope(403, `{"msg":"biz-jwt rejected"}`)
		}
		return baseRespond(call)
	}
	started := authStart(t)
	resp := authPoll(t, started.State)
	if resp.Status != pluginapi.AuthLoginStatusError || strings.Contains(resp.Message, "biz-jwt") || !strings.Contains(resp.Message, "[redacted]") {
		t.Fatalf("poll = %+v, want redacted key-provisioning failure", resp)
	}
	for _, call := range fake.recorded() {
		if strings.HasSuffix(call.URL, "/api_keys") && call.Method != http.MethodGet {
			t.Fatalf("failed key list led to key creation: %s %s", call.Method, call.URL)
		}
	}
}

// Acceptance: organization and project metadata survive login persistence and
// later auth.parse normalization.
func TestLoginMetadataSurvivesStorageParse(t *testing.T) {
	resetAuthFlows()
	t.Cleanup(resetAuthFlows)
	authLoginRoutes(t, func() string { return authReadyBody() })
	started := authStart(t)
	login := authPoll(t, started.State)
	if login.Status != pluginapi.AuthLoginStatusSuccess {
		t.Fatalf("login status = %q (%s), want success", login.Status, login.Message)
	}
	payload := fmt.Sprintf(`{"RawJSON":%q}`, base64.StdEncoding.EncodeToString(login.Auth.StorageJSON))
	raw, errHandle := callAuthMethod(t, pluginabi.MethodAuthParse, payload)
	if errHandle != nil {
		t.Fatalf("auth.parse failed: %v", errHandle)
	}
	var parsed pluginapi.AuthParseResponse
	decodeAuthResult(t, raw, &parsed)
	if !parsed.Handled {
		t.Fatalf("login storage not handled: %+v", parsed)
	}
	if len(parsed.Auth.Metadata) != 3 || parsed.Auth.Metadata["type"] != providerID || parsed.Auth.Metadata["organization_id"] != "org-default" || parsed.Auth.Metadata["project_id"] != "proj-default" {
		t.Fatalf("parsed Auth.Metadata = %v", parsed.Auth.Metadata)
	}
}

func TestFetchCustomerOrgAcceptsIDOnlyOrganization(t *testing.T) {
	installAuthFakeHost(t, func(call authHostCall) []byte {
		return authHostEnvelope(200, `{"code":200,"data":{"organizations":[{"id":"org-id-only","isDefault":true,"projects":[{"id":"proj-id-only"}]}]}}`)
	})
	orgID, projectID := fetchCustomerOrg("cb-1", "biz-jwt")
	if orgID != "org-id-only" || projectID != "proj-id-only" {
		t.Fatalf("organization/project = %q/%q", orgID, projectID)
	}
}

func TestFetchCustomerOrgRequiresDefaultOrganization(t *testing.T) {
	installAuthFakeHost(t, func(call authHostCall) []byte {
		return authHostEnvelope(200, `{"code":200,"data":{"organizations":[{"organizationId":"org-first","isDefault":false,"projects":[{"projectId":"proj-first"}]}]}}`)
	})
	orgID, projectID := fetchCustomerOrg("cb-1", "biz-jwt")
	if orgID != "" || projectID != "" {
		t.Fatalf("organization/project = %q/%q, want missing default context", orgID, projectID)
	}
}

// Acceptance: a second poll inside the upstream interval must skip upstream.
func TestPollRateLimitSkipsUpstream(t *testing.T) {
	resetAuthFlows()
	t.Cleanup(resetAuthFlows)
	fake := authLoginRoutes(t, func() string { return `{"code":0,"data":{"status":"pending"}}` })
	started := authStart(t)
	if resp := authPoll(t, started.State); resp.Status != pluginapi.AuthLoginStatusPending {
		t.Fatalf("first poll status = %q", resp.Status)
	}
	if resp := authPoll(t, started.State); resp.Status != pluginapi.AuthLoginStatusPending {
		t.Fatalf("second poll status = %q", resp.Status)
	}
	if got := fake.countURL(cliPollURL); got != 1 {
		t.Fatalf("upstream poll calls = %d, want 1", got)
	}
}

// Acceptance: an expired flow fails the poll without contacting upstream.
func TestPollExpiredFlow(t *testing.T) {
	resetAuthFlows()
	t.Cleanup(resetAuthFlows)
	fake := installAuthFakeHost(t, func(call authHostCall) []byte {
		if call.URL == cliInitURL {
			return authHostEnvelope(200, authInitBody("2020-01-01T00:00:00Z", 0))
		}
		t.Errorf("unexpected upstream call %s %s", call.Method, call.URL)
		return authHostEnvelope(500, `{"code":500,"msg":"must not poll"}`)
	})
	started := authStart(t)
	if !started.ExpiresAt.After(time.Now()) {
		t.Fatalf("start must sanitize a past upstream expiry, got %v", started.ExpiresAt)
	}
	flowsMu.Lock()
	flows[started.State].expiresAt = time.Now().Add(-time.Second)
	flowsMu.Unlock()
	resp := authPoll(t, started.State)
	if resp.Status != pluginapi.AuthLoginStatusError || !strings.Contains(resp.Message, "expired") {
		t.Fatalf("poll = %+v, want expired error", resp)
	}
	if fake.countURL(cliPollURL) != 0 {
		t.Fatalf("expired flow contacted upstream %d times", fake.countURL(cliPollURL))
	}
}

// Acceptance: an unknown state fails without any upstream call.
func TestPollUnknownState(t *testing.T) {
	resetAuthFlows()
	t.Cleanup(resetAuthFlows)
	fake := installAuthFakeHost(t, func(call authHostCall) []byte {
		t.Errorf("unexpected upstream call %s %s", call.Method, call.URL)
		return authHostEnvelope(500, `{"code":500,"msg":"must not call"}`)
	})
	resp := authPoll(t, "missing-state")
	if resp.Status != pluginapi.AuthLoginStatusError || !strings.Contains(resp.Message, "not found") {
		t.Fatalf("poll = %+v, want not-found error", resp)
	}
	if len(fake.recorded()) != 0 {
		t.Fatalf("unknown state must not contact upstream")
	}
}

// Acceptance: ten concurrent polls on one state exchange exactly once.
func TestConcurrentPollSingleExchange(t *testing.T) {
	resetAuthFlows()
	t.Cleanup(resetAuthFlows)
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	fake := installAuthFakeHost(t, func(call authHostCall) []byte {
		switch {
		case call.URL == cliInitURL:
			return authHostEnvelope(200, authInitBody(nil, 0))
		case strings.HasPrefix(call.URL, cliPollURL):
			select {
			case entered <- struct{}{}:
			default:
			}
			<-release
			return authHostEnvelope(200, authReadyBody())
		case call.URL == businessLoginURL:
			return authHostEnvelope(200, `{"code":200,"data":{"access_token":"biz-jwt"}}`)
		case call.URL == customerInfoURL:
			return authHostEnvelope(200, `{"code":200,"data":{"organizations":[{"organizationId":"org-1","isDefault":true,"projects":[{"projectId":"proj-1"}]}]}}`)
		case call.URL == "https://api.z.ai/api/biz/v1/organization/org-1/projects/proj-1/api_keys":
			return authHostEnvelope(200, `{"code":200,"data":[{"name":"cpa-auth-zai","apiKey":"key-id","secretKey":"key-secret"}]}`)
		}
		t.Errorf("unexpected upstream call %s %s", call.Method, call.URL)
		return authHostEnvelope(404, `{"code":404,"msg":"not found"}`)
	})
	started := authStart(t)
	const workers = 10
	startLine := make(chan struct{})
	results := make(chan pluginapi.AuthLoginPollResponse, workers)
	for range workers {
		go func() {
			<-startLine
			payload, _ := json.Marshal(map[string]any{"Provider": "zai", "State": started.State, "host_callback_id": "cb-1"})
			raw, errHandle := handleCall(pluginabi.MethodAuthLoginPoll, payload)
			if errHandle != nil {
				results <- pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusError, Message: errHandle.Error()}
				return
			}
			var envelope pluginabi.Envelope
			if errUnmarshal := json.Unmarshal(raw, &envelope); errUnmarshal != nil || !envelope.OK {
				results <- pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusError, Message: "bad envelope"}
				return
			}
			var resp pluginapi.AuthLoginPollResponse
			if errUnmarshal := json.Unmarshal(envelope.Result, &resp); errUnmarshal != nil {
				results <- pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusError, Message: errUnmarshal.Error()}
				return
			}
			results <- resp
		}()
	}
	close(startLine)
	<-entered
	for pending := 0; pending < workers-1; pending++ {
		if resp := <-results; resp.Status != pluginapi.AuthLoginStatusPending {
			t.Fatalf("in-flight poll returned %q (%s), want pending", resp.Status, resp.Message)
		}
	}
	close(release)
	if resp := <-results; resp.Status != pluginapi.AuthLoginStatusSuccess {
		t.Fatalf("exchange poll returned %q (%s), want success", resp.Status, resp.Message)
	}
	if got := fake.countURL(cliPollURL); got != 1 {
		t.Fatalf("upstream poll calls = %d, want exactly 1", got)
	}
}

// Acceptance: z.ai has no verified token refresh; renewal requires a new
// browser sign-in and must not contact upstream.
func TestRefreshRequiresNewBrowserSignIn(t *testing.T) {
	resetAuthFlows()
	t.Cleanup(resetAuthFlows)
	fake := installAuthFakeHost(t, func(call authHostCall) []byte {
		t.Errorf("refresh must not contact upstream: %s %s", call.Method, call.URL)
		return authHostEnvelope(500, `{"code":500,"msg":"must not call"}`)
	})
	storageJSON := `{"version":1,"type":"zai","oauth_access_token":"oauth-tok","email":"` + authTestEmail + `"}`
	payload := fmt.Sprintf(`{"AuthID":"zai-user-example-com","StorageJSON":%q}`, base64.StdEncoding.EncodeToString([]byte(storageJSON)))
	perr := testCallErr(t, pluginabi.MethodAuthRefresh, []byte(payload))
	if perr.Code != "unsupported_operation" || perr.Message != "z.ai login renewal requires a new browser sign-in" || perr.HTTPStatus != http.StatusNotImplemented {
		t.Fatalf("auth.refresh error = %+v", perr)
	}
	if calls := len(fake.recorded()); calls != 0 {
		t.Fatalf("refresh made %d upstream calls, want 0", calls)
	}
}

// Acceptance: login provisions one key; parse and refresh make no upstream calls.
func TestKeyManagementOnlyDuringLogin(t *testing.T) {
	resetAuthFlows()
	t.Cleanup(resetAuthFlows)
	loginFake := authLoginRoutes(t, func() string { return authReadyBody() })
	started := authStart(t)
	if resp := authPoll(t, started.State); resp.Status != pluginapi.AuthLoginStatusSuccess {
		t.Fatalf("login poll status = %q (%s)", resp.Status, resp.Message)
	}
	if got := loginFake.countURL("https://api.z.ai/api/biz/v1/organization/org-default/projects/proj-default/api_keys"); got != 1 {
		t.Fatalf("login key-list calls = %d, want 1", got)
	}
	refreshFake := installAuthFakeHost(t, func(call authHostCall) []byte {
		t.Errorf("parse/refresh must not contact upstream: %s %s", call.Method, call.URL)
		return authHostEnvelope(500, `{"code":500,"msg":"must not call"}`)
	})
	storageJSON := `{"version":1,"type":"zai","oauth_access_token":"oauth-tok","email":"` + authTestEmail + `"}`
	refreshPayload := fmt.Sprintf(`{"StorageJSON":%q}`, base64.StdEncoding.EncodeToString([]byte(storageJSON)))
	if perr := testCallErr(t, pluginabi.MethodAuthRefresh, []byte(refreshPayload)); perr.Code != "unsupported_operation" {
		t.Fatalf("auth.refresh error = %+v, want unsupported_operation", perr)
	}
	parsePayload := fmt.Sprintf(`{"RawJSON":%q}`, base64.StdEncoding.EncodeToString([]byte(storageJSON)))
	if _, errHandle := callAuthMethod(t, pluginabi.MethodAuthParse, parsePayload); errHandle != nil {
		t.Fatalf("auth.parse failed: %v", errHandle)
	}
	if calls := len(refreshFake.recorded()); calls != 0 {
		t.Fatalf("parse/refresh made %d upstream calls, want 0", calls)
	}
}

// Acceptance: parse accepts a zai storage record and rebuilds normalized auth
// data with the provider identity.
func TestParseAuthHandlesZaiStorage(t *testing.T) {
	resetAuthFlows()
	t.Cleanup(resetAuthFlows)
	installAuthFakeHost(t, func(call authHostCall) []byte {
		t.Errorf("parse must not contact upstream: %s %s", call.Method, call.URL)
		return authHostEnvelope(500, `{"code":500,"msg":"must not call"}`)
	})
	storageJSON := `{"version":1,"type":"zai","oauth_access_token":"oauth-tok","email":"` + authTestEmail + `","extra_field":"dropped"}`
	payload := fmt.Sprintf(`{"RawJSON":%q}`, base64.StdEncoding.EncodeToString([]byte(storageJSON)))
	raw, errHandle := callAuthMethod(t, pluginabi.MethodAuthParse, payload)
	if errHandle != nil {
		t.Fatalf("auth.parse failed: %v", errHandle)
	}
	var resp pluginapi.AuthParseResponse
	decodeAuthResult(t, raw, &resp)
	if !resp.Handled {
		t.Fatalf("zai storage not handled: %+v", resp)
	}
	if resp.Auth.Provider != providerID || resp.Auth.ID != "zai-user-example-com" {
		t.Fatalf("Auth = %+v", resp.Auth)
	}
	if resp.Auth.FileName != "zai-user-example-com.json" || resp.Auth.Label != "Z.ai "+authTestEmail {
		t.Fatalf("Auth file/label = %q / %q", resp.Auth.FileName, resp.Auth.Label)
	}
	var storage zaiStorage
	if errUnmarshal := json.Unmarshal(resp.Auth.StorageJSON, &storage); errUnmarshal != nil {
		t.Fatalf("decode StorageJSON: %v", errUnmarshal)
	}
	if storage.OAuthAccessToken != "oauth-tok" || storage.Email != authTestEmail {
		t.Fatalf("normalized storage = %+v", storage)
	}
	if resp.Auth.Metadata["type"] != providerID {
		t.Fatalf("Auth.Metadata = %v", resp.Auth.Metadata)
	}
}

// Acceptance: foreign or tokenless material is not handled and never contacts
// upstream.
func TestParseAuthRejectsForeign(t *testing.T) {
	resetAuthFlows()
	t.Cleanup(resetAuthFlows)
	installAuthFakeHost(t, func(call authHostCall) []byte {
		t.Errorf("parse must not contact upstream: %s %s", call.Method, call.URL)
		return authHostEnvelope(500, `{"code":500,"msg":"must not call"}`)
	})
	for name, raw := range map[string]string{
		"foreign type":  `{"version":1,"type":"other","api_key":"k"}`,
		"tokenless zai": `{"version":1,"type":"zai"}`,
		"not json":      `{{{`,
	} {
		payload := fmt.Sprintf(`{"RawJSON":%q}`, base64.StdEncoding.EncodeToString([]byte(raw)))
		respRaw, errHandle := handleCall(pluginabi.MethodAuthParse, []byte(payload))
		if errHandle != nil {
			t.Fatalf("%s: auth.parse failed: %v", name, errHandle)
		}
		var envelope pluginabi.Envelope
		if errUnmarshal := json.Unmarshal(respRaw, &envelope); errUnmarshal != nil || !envelope.OK {
			t.Fatalf("%s: envelope not ok: %s", name, respRaw)
		}
		var resp pluginapi.AuthParseResponse
		decodeAuthResult(t, envelope.Result, &resp)
		if resp.Handled {
			t.Fatalf("%s: foreign material handled: %+v", name, resp)
		}
	}
}
