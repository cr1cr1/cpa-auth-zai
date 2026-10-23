package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
)

// mgmtHistoryPayload builds a management.handle request for the history
// route, shaped as the host sends it.
func mgmtHistoryPayload(t *testing.T, query map[string][]string) []byte {
	t.Helper()
	raw, errMarshal := json.Marshal(map[string]any{
		"Method":           "GET",
		"Path":             "/v0/management" + usageHistoryRoutePath,
		"Query":            query,
		"host_callback_id": "cb-1",
	})
	if errMarshal != nil {
		t.Fatalf("marshal management payload: %v", errMarshal)
	}
	return raw
}

func mgmtResult(t *testing.T, raw []byte) (status int, body []byte) {
	t.Helper()
	var envelope pluginabi.Envelope
	if errUnmarshal := json.Unmarshal(raw, &envelope); errUnmarshal != nil || !envelope.OK {
		t.Fatalf("bad management envelope %s: %v", raw, errUnmarshal)
	}
	var response struct {
		StatusCode int                 `json:"StatusCode"`
		Headers    map[string][]string `json:"Headers"`
		Body       []byte              `json:"Body"`
	}
	if errUnmarshal := json.Unmarshal(envelope.Result, &response); errUnmarshal != nil {
		t.Fatalf("decode management response %s: %v", envelope.Result, errUnmarshal)
	}
	return response.StatusCode, response.Body
}

// Acceptance: registration exposes exactly one authenticated management
// route — no unauthenticated resource route for account usage data.
func TestManagementRegistersProtectedRouteOnly(t *testing.T) {
	raw := callResult(t, pluginabi.MethodManagementRegister, []byte("{}"))
	var registration struct {
		Routes []struct {
			Method      string `json:"Method"`
			Path        string `json:"Path"`
			Menu        string `json:"Menu"`
			Description string `json:"Description"`
		} `json:"routes"`
		Resources []map[string]any `json:"resources"`
	}
	if errUnmarshal := json.Unmarshal(raw, &registration); errUnmarshal != nil {
		t.Fatalf("decode registration: %v", errUnmarshal)
	}
	if len(registration.Routes) != 1 {
		t.Fatalf("routes = %d, want 1: %s", len(registration.Routes), raw)
	}
	route := registration.Routes[0]
	if route.Method != "GET" {
		t.Fatalf("method = %q, want GET", route.Method)
	}
	// The host joins relative paths under /v0/management, so the final URL is
	// /v0/management/plugins/cpa-auth-zai/usage/history.
	if route.Path != usageHistoryRoutePath || usageHistoryRoutePath != "/plugins/cpa-auth-zai/usage/history" {
		t.Fatalf("path = %q, want %q", route.Path, usageHistoryRoutePath)
	}
	if route.Menu != "" {
		t.Fatalf("menu = %q, want empty so the route stays out of /v0/resource", route.Menu)
	}
	if !strings.Contains(route.Description, "account-scoped") {
		t.Fatalf("description = %q", route.Description)
	}
	if len(registration.Resources) != 0 {
		t.Fatalf("usage data must never be a public resource: %v", registration.Resources)
	}
}

// Acceptance: the history route requires auth_index.
func TestHistoryRequiresAuthIndex(t *testing.T) {
	swapHostCaller(t, func(method string, payload []byte) ([]byte, error) {
		t.Errorf("no host call expected, got %s", method)
		return nil, nil
	})
	raw, errHandle := handleCall(pluginabi.MethodManagementHandle, mgmtHistoryPayload(t, map[string][]string{}))
	if errHandle != nil {
		t.Fatalf("management.handle: %v", errHandle)
	}
	status, body := mgmtResult(t, raw)
	if status != 400 {
		t.Fatalf("status = %d, want 400 (%s)", status, body)
	}
	if !strings.Contains(string(body), "auth_index") {
		t.Fatalf("body must name the missing parameter: %s", body)
	}
}

// Acceptance: reversed, unbounded, and missing ranges are rejected.
func TestHistoryRangeValidation(t *testing.T) {
	swapHostCaller(t, func(method string, payload []byte) ([]byte, error) {
		t.Errorf("no host call expected, got %s", method)
		return nil, nil
	})
	cases := []struct {
		name  string
		query map[string][]string
	}{
		{"missing start", map[string][]string{"auth_index": {"idx-1"}, "end": {"2026-10-24T00:00:00+08:00"}}},
		{"invalid start", map[string][]string{"auth_index": {"idx-1"}, "start": {"yesterday"}, "end": {"2026-10-24T00:00:00+08:00"}}},
		{"end before start", map[string][]string{
			"auth_index": {"idx-1"},
			"start":      {"2026-10-24T00:00:00+08:00"},
			"end":        {"2026-10-17T00:00:00+08:00"},
		}},
		{"range over 366 days", map[string][]string{
			"auth_index": {"idx-1"},
			"start":      {"2025-01-01T00:00:00+08:00"},
			"end":        {"2026-10-24T00:00:00+08:00"},
		}},
	}
	for _, testCase := range cases {
		raw, errHandle := handleCall(pluginabi.MethodManagementHandle, mgmtHistoryPayload(t, testCase.query))
		if errHandle != nil {
			t.Fatalf("%s: management.handle: %v", testCase.name, errHandle)
		}
		status, body := mgmtResult(t, raw)
		if status != 400 {
			t.Fatalf("%s: status = %d, want 400 (%s)", testCase.name, status, body)
		}
	}
}

// Acceptance: an unknown timezone query is rejected with 400.
func TestHistoryUnknownTimezone400(t *testing.T) {
	swapHostCaller(t, func(method string, payload []byte) ([]byte, error) {
		t.Errorf("no host call expected, got %s", method)
		return nil, nil
	})
	raw, errHandle := handleCall(pluginabi.MethodManagementHandle, mgmtHistoryPayload(t, map[string][]string{
		"auth_index": {"idx-1"},
		"start":      {"2026-10-17T00:00:00+08:00"},
		"end":        {"2026-10-18T00:00:00+08:00"},
		"timezone":   {"Mars/Olympus"},
	}))
	if errHandle != nil {
		t.Fatalf("management.handle: %v", errHandle)
	}
	status, _ := mgmtResult(t, raw)
	if status != 400 {
		t.Fatalf("status = %d, want 400", status)
	}
}

// Acceptance: the endpoint resolves credentials through host.auth.get, pulls
// usage-detail from upstream, and returns the fixture-backed view.
func TestHistoryReturnsFixtureView(t *testing.T) {
	storageJSON := []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-oauth","email":"a@b.c"}`)
	fixture := usageFixtureBytes(t)
	var upstreamURL string
	swapHostCaller(t, func(method string, payload []byte) ([]byte, error) {
		switch method {
		case pluginabi.MethodHostAuthGet:
			var request struct {
				AuthIndex string `json:"auth_index"`
			}
			if errUnmarshal := json.Unmarshal(payload, &request); errUnmarshal != nil {
				t.Fatalf("decode host.auth.get payload: %v", errUnmarshal)
			}
			if request.AuthIndex != "idx-1" {
				t.Fatalf("auth_index = %q, want idx-1", request.AuthIndex)
			}
			return hostEnvelope(t, map[string]any{
				"auth_index": "idx-1",
				"name":       "a-b-c.json",
				"json":       json.RawMessage(storageJSON),
			}), nil
		case pluginabi.MethodHostHTTPDo:
			var request hostHTTPRequest
			if errUnmarshal := json.Unmarshal(payload, &request); errUnmarshal != nil {
				t.Fatalf("decode host.http.do payload: %v", errUnmarshal)
			}
			upstreamURL = request.Request.URL
			if auth := request.Request.Headers["Authorization"][0]; auth != "Bearer tok-oauth" {
				t.Fatalf("authorization = %q", auth)
			}
			return upstreamHTTP(t, 200, fixture), nil
		}
		t.Fatalf("unexpected host call %s", method)
		return nil, nil
	})
	raw, errHandle := handleCall(pluginabi.MethodManagementHandle, mgmtHistoryPayload(t, map[string][]string{
		"auth_index": {"idx-1"},
		"start":      {"2026-10-17T00:00:00+08:00"},
		"end":        {"2026-10-24T00:00:00+08:00"},
		"timezone":   {"Europe/Bucharest"},
	}))
	if errHandle != nil {
		t.Fatalf("management.handle: %v", errHandle)
	}
	status, body := mgmtResult(t, raw)
	if !strings.HasPrefix(upstreamURL, usageDetailURL+"?") ||
		!strings.Contains(upstreamURL, "usageType=MODEL") || !strings.Contains(upstreamURL, "type=1") {
		t.Fatalf("upstream URL = %q", upstreamURL)
	}
	if status != 200 {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}
	var view struct {
		RequestedTimezone string `json:"requested_timezone"`
		SourceTimezone    string `json:"source_timezone"`
		Summary           struct {
			TotalTokens struct {
				Value string `json:"value"`
			} `json:"total_tokens"`
		} `json:"summary"`
		Models []struct {
			Code   string `json:"code"`
			Totals struct {
				Total int64 `json:"total"`
			} `json:"totals"`
		} `json:"models"`
	}
	if errUnmarshal := json.Unmarshal(body, &view); errUnmarshal != nil {
		t.Fatalf("decode view: %v", errUnmarshal)
	}
	if view.RequestedTimezone != "Europe/Bucharest" || view.SourceTimezone != "Asia/Shanghai" {
		t.Fatalf("timezones = %q / %q", view.RequestedTimezone, view.SourceTimezone)
	}
	if view.Summary.TotalTokens.Value != "43826245" {
		t.Fatalf("summary total = %q", view.Summary.TotalTokens.Value)
	}
	if len(view.Models) != 2 ||
		view.Models[0].Code != "glm-5.3" || view.Models[0].Totals.Total != 43484230 ||
		view.Models[1].Code != "glm-5.3-flash" || view.Models[1].Totals.Total != 342015 {
		t.Fatalf("models = %+v", view.Models)
	}
}

// Acceptance: an unknown auth_index is a 404, never a data error.
func TestHistoryAuthNotFound404(t *testing.T) {
	swapHostCaller(t, func(method string, payload []byte) ([]byte, error) {
		if method != pluginabi.MethodHostAuthGet {
			t.Fatalf("unexpected host call %s", method)
		}
		return json.Marshal(pluginabi.Envelope{OK: false, Error: pluginabi.NewError("not_found", "auth not found for auth_index idx-404", 0)})
	})
	raw, errHandle := handleCall(pluginabi.MethodManagementHandle, mgmtHistoryPayload(t, map[string][]string{
		"auth_index": {"idx-404"},
		"start":      {"2026-10-17T00:00:00+08:00"},
		"end":        {"2026-10-18T00:00:00+08:00"},
	}))
	if errHandle != nil {
		t.Fatalf("management.handle: %v", errHandle)
	}
	status, body := mgmtResult(t, raw)
	if status != 404 {
		t.Fatalf("status = %d, want 404 (%s)", status, body)
	}
	if !strings.Contains(string(body), "auth not found") {
		t.Fatalf("body = %s", body)
	}
}
