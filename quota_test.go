package main

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
)

// hostEnvelope wraps a fake host callback result in the pluginabi envelope
// the real host produces: {"ok":true,"result":<raw json>}.
func hostEnvelope(t *testing.T, result any) []byte {
	t.Helper()
	raw, errMarshal := json.Marshal(result)
	if errMarshal != nil {
		t.Fatalf("marshal host result: %v", errMarshal)
	}
	env, errMarshal := json.Marshal(pluginabi.Envelope{OK: true, Result: raw})
	if errMarshal != nil {
		t.Fatalf("marshal host envelope: %v", errMarshal)
	}
	return env
}

// callResult invokes one plugin method and unwraps its result envelope.
func callResult(t *testing.T, method string, payload []byte) []byte {
	t.Helper()
	raw, errHandle := handleCall(method, payload)
	if errHandle != nil {
		t.Fatalf("%s: %v", method, errHandle)
	}
	var envelope pluginabi.Envelope
	if errUnmarshal := json.Unmarshal(raw, &envelope); errUnmarshal != nil || !envelope.OK {
		t.Fatalf("%s: bad envelope %s: %v", method, raw, errUnmarshal)
	}
	return envelope.Result
}

// swapHostCaller installs a fake host caller for the duration of the test.
func swapHostCaller(t *testing.T, fn func(method string, payload []byte) ([]byte, error)) {
	t.Helper()
	previous := hostCaller
	hostCaller = fn
	t.Cleanup(func() { hostCaller = previous })
}

// quotaFetchPayload builds a quota.fetch request whose storage_json carries
// two bearer candidates, exactly as the host sends it (base64 []byte).
func quotaFetchPayload(t *testing.T) []byte {
	t.Helper()
	storage, errMarshal := json.Marshal(zaiStorage{Version: storageVersion, Type: providerID, OAuthAccessToken: "tok-oauth", BizAccessToken: "tok-biz"})
	if errMarshal != nil {
		t.Fatalf("marshal storage: %v", errMarshal)
	}
	raw, errMarshal := json.Marshal(struct {
		AuthIndex      string `json:"auth_index"`
		Provider       string `json:"provider"`
		StorageJSON    []byte `json:"storage_json"`
		HostCallbackID string `json:"host_callback_id"`
	}{AuthIndex: "idx-1", Provider: providerID, StorageJSON: storage, HostCallbackID: "cb-1"})
	if errMarshal != nil {
		t.Fatalf("marshal quota payload: %v", errMarshal)
	}
	return raw
}

// quotaLimitsBody scripts an upstream quota/limit response.
func quotaLimitsBody(t *testing.T, limits string) []byte {
	t.Helper()
	return []byte(`{"code":200,"success":true,"msg":"ok","data":{"subscriptionStatus":"glm_coding_plan","limits":[` + limits + `]}}`)
}

func upstreamHTTP(t *testing.T, status int, body []byte) []byte {
	t.Helper()
	return hostEnvelope(t, hostHTTPResponse{StatusCode: status, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: body})
}

func decodeQuotaResult(t *testing.T, raw []byte) map[string]json.RawMessage {
	t.Helper()
	var result map[string]json.RawMessage
	if errUnmarshal := json.Unmarshal(raw, &result); errUnmarshal != nil {
		t.Fatalf("decode quota result %s: %v", raw, errUnmarshal)
	}
	return result
}

// Acceptance: quota.describe and quota.identifier pin the provider identity.
func TestQuotaDescribeShape(t *testing.T) {
	raw := callResult(t, pluginabi.MethodQuotaIdentifier, []byte("{}"))
	var identifier struct {
		Identifier string `json:"identifier"`
	}
	if errUnmarshal := json.Unmarshal(raw, &identifier); errUnmarshal != nil {
		t.Fatalf("decode identifier: %v", errUnmarshal)
	}
	if identifier.Identifier != providerID {
		t.Fatalf("identifier = %q, want %q", identifier.Identifier, providerID)
	}

	raw = callResult(t, pluginabi.MethodQuotaDescribe, []byte("{}"))
	var describe struct {
		SupportedProviders []string `json:"supported_providers"`
		DisplayName        string   `json:"display_name"`
		SupportsReset      *bool    `json:"supports_reset"`
	}
	if errUnmarshal := json.Unmarshal(raw, &describe); errUnmarshal != nil {
		t.Fatalf("decode describe: %v", errUnmarshal)
	}
	if len(describe.SupportedProviders) != 1 || describe.SupportedProviders[0] != providerID {
		t.Fatalf("supported_providers = %v", describe.SupportedProviders)
	}
	if describe.DisplayName != "Z.ai" {
		t.Fatalf("display_name = %q", describe.DisplayName)
	}
	if describe.SupportsReset == nil || *describe.SupportsReset {
		t.Fatalf("supports_reset = %v, want explicit false", describe.SupportsReset)
	}
}

// Acceptance: mapped windows produce buckets and summary metrics with the
// upstream percentage, reset instant, and plan passthrough.
func TestQuotaFetchMapsWindows(t *testing.T) {
	var authHeaders []string
	swapHostCaller(t, func(method string, payload []byte) ([]byte, error) {
		if method != pluginabi.MethodHostHTTPDo {
			t.Fatalf("unexpected host call %s", method)
		}
		var request hostHTTPRequest
		if errUnmarshal := json.Unmarshal(payload, &request); errUnmarshal != nil {
			t.Fatalf("decode host payload: %v", errUnmarshal)
		}
		if !strings.HasPrefix(request.Request.URL, quotaLimitURL) {
			t.Fatalf("upstream URL = %q", request.Request.URL)
		}
		authHeaders = append(authHeaders, request.Request.Headers["Authorization"][0])
		return upstreamHTTP(t, 200, quotaLimitsBody(t,
			`{"unit":3,"number":5,"usage":12.5,"percentage":34.2,"currentValue":"12.5","nextResetTime":1770000000000,"limit":100},`+
				`{"unit":6,"number":1,"percentage":0,"usage":3,"limit":100}`)), nil
	})
	raw := callResult(t, pluginabi.MethodQuotaFetch, quotaFetchPayload(t))
	if len(authHeaders) != 1 || authHeaders[0] != "Bearer tok-oauth" {
		t.Fatalf("authorization headers = %v", authHeaders)
	}
	var result struct {
		Subscription *struct {
			Plan string `json:"plan"`
		} `json:"subscription"`
		Summary []struct {
			Key    string  `json:"key"`
			Label  string  `json:"label"`
			Value  float64 `json:"value"`
			Format string  `json:"format"`
		} `json:"summary"`
		Groups []struct {
			DisplayName string `json:"displayName"`
			Buckets     []struct {
				Window            string  `json:"window"`
				RemainingFraction float64 `json:"remainingFraction"`
				ResetTime         string  `json:"resetTime"`
				Description       string  `json:"description"`
			} `json:"buckets"`
		} `json:"groups"`
	}
	if errUnmarshal := json.Unmarshal(raw, &result); errUnmarshal != nil {
		t.Fatalf("decode fetch result: %v", errUnmarshal)
	}
	if result.Subscription == nil || result.Subscription.Plan != "glm_coding_plan" {
		t.Fatalf("subscription = %+v", result.Subscription)
	}
	if len(result.Groups) != 1 || result.Groups[0].DisplayName != "Usage windows" || len(result.Groups[0].Buckets) != 2 {
		t.Fatalf("groups = %+v", result.Groups)
	}
	fiveHour := result.Groups[0].Buckets[0]
	sevenDay := result.Groups[0].Buckets[1]
	if fiveHour.Window != "5h" || sevenDay.Window != "7d" {
		t.Fatalf("windows = %q / %q", fiveHour.Window, sevenDay.Window)
	}
	if math.Abs(fiveHour.RemainingFraction-0.658) > 1e-9 {
		t.Fatalf("5h remainingFraction = %v, want 0.658", fiveHour.RemainingFraction)
	}
	if sevenDay.RemainingFraction != 1 {
		t.Fatalf("7d remainingFraction = %v, want 1", sevenDay.RemainingFraction)
	}
	if fiveHour.Description != "Rolling 5-hour window" || sevenDay.Description != "Rolling 7-day window" {
		t.Fatalf("descriptions = %q / %q", fiveHour.Description, sevenDay.Description)
	}
	if fiveHour.ResetTime != time.Unix(1770000000, 0).UTC().Format(time.RFC3339) {
		t.Fatalf("5h resetTime = %q", fiveHour.ResetTime)
	}
	if sevenDay.ResetTime != "" {
		t.Fatalf("7d resetTime = %q, want absent", sevenDay.ResetTime)
	}
	if len(result.Summary) != 2 {
		t.Fatalf("summary = %+v", result.Summary)
	}
	if result.Summary[0].Key != "usage_5h" || result.Summary[0].Label != "5-hour usage %" ||
		math.Abs(result.Summary[0].Value-34.2) > 1e-9 || result.Summary[0].Format != "number" {
		t.Fatalf("5h metric = %+v", result.Summary[0])
	}
	if result.Summary[1].Key != "usage_7d" || result.Summary[1].Label != "7-day usage %" || result.Summary[1].Value != 0 {
		t.Fatalf("7d metric = %+v", result.Summary[1])
	}
}

// Acceptance: unmapped unit/number pairs are ignored, never mislabeled.
func TestQuotaFetchIgnoresUnknownUnitPairs(t *testing.T) {
	swapHostCaller(t, func(method string, payload []byte) ([]byte, error) {
		return upstreamHTTP(t, 200, quotaLimitsBody(t,
			`{"unit":2,"number":1,"percentage":50},`+
				`{"unit":3,"number":4,"percentage":50},`+
				`{"unit":6,"number":7,"percentage":50}`)), nil
	})
	raw := callResult(t, pluginabi.MethodQuotaFetch, quotaFetchPayload(t))
	result := decodeQuotaResult(t, raw)
	if _, exists := result["groups"]; exists {
		t.Fatalf("unknown pairs must not produce buckets: %s", raw)
	}
	if _, exists := result["summary"]; exists {
		t.Fatalf("unknown pairs must not produce metrics: %s", raw)
	}
}

// Acceptance: a 401 from the first bearer candidate advances to the next
// candidate; a 2xx stops the loop.
func TestQuotaFetchAuthFallsBackOn401(t *testing.T) {
	var authHeaders []string
	swapHostCaller(t, func(method string, payload []byte) ([]byte, error) {
		var request hostHTTPRequest
		if errUnmarshal := json.Unmarshal(payload, &request); errUnmarshal != nil {
			t.Fatalf("decode host payload: %v", errUnmarshal)
		}
		authHeaders = append(authHeaders, request.Request.Headers["Authorization"][0])
		if len(authHeaders) == 1 {
			return upstreamHTTP(t, 401, []byte(`{"code":401,"msg":"token expired"}`)), nil
		}
		return upstreamHTTP(t, 200, quotaLimitsBody(t, `{"unit":3,"number":5,"percentage":10}`)), nil
	})
	raw := callResult(t, pluginabi.MethodQuotaFetch, quotaFetchPayload(t))
	if len(authHeaders) != 2 || authHeaders[0] != "Bearer tok-oauth" || authHeaders[1] != "Bearer tok-biz" {
		t.Fatalf("authorization headers = %v", authHeaders)
	}
	result := decodeQuotaResult(t, raw)
	if _, exists := result["groups"]; !exists {
		t.Fatalf("second candidate must succeed: %s", raw)
	}
}

// Acceptance: every candidate rejected leaves quota unknown — an empty
// successful response, never zero-filled buckets.
func TestQuotaFetchExhaustedIsEmpty(t *testing.T) {
	calls := 0
	swapHostCaller(t, func(method string, payload []byte) ([]byte, error) {
		calls++
		return upstreamHTTP(t, 401, []byte(`{"code":401,"msg":"unauthorized"}`)), nil
	})
	raw := callResult(t, pluginabi.MethodQuotaFetch, quotaFetchPayload(t))
	if calls != 2 {
		t.Fatalf("upstream calls = %d, want 2", calls)
	}
	result := decodeQuotaResult(t, raw)
	if len(result) != 0 {
		t.Fatalf("exhausted candidates must return an empty object, got %s", raw)
	}
}

// Acceptance: quota.reset is unsupported and makes no upstream call.
func TestQuotaResetUnsupportedNoUpstream(t *testing.T) {
	swapHostCaller(t, func(method string, payload []byte) ([]byte, error) {
		t.Errorf("quota.reset must not call the host, got %s", method)
		return nil, nil
	})
	_, errHandle := handleCall(pluginabi.MethodQuotaReset, []byte("{}"))
	if errHandle == nil {
		t.Fatalf("quota.reset must fail")
	}
	var pluginErr *pluginabi.Error
	if !errors.As(errHandle, &pluginErr) {
		t.Fatalf("error must be a pluginabi.Error, got %T", errHandle)
	}
	if pluginErr.Code != "unsupported_operation" {
		t.Fatalf("code = %q, want unsupported_operation", pluginErr.Code)
	}
	if pluginErr.StatusCode() != 501 {
		t.Fatalf("http status = %d, want 501", pluginErr.StatusCode())
	}
}

// Acceptance: when the host sends quota.fetch without storage_json (the
// credential quota path does not embed it), the plugin resolves the credential
// through host.auth.get using auth_index before treating quota as unknown.
func TestQuotaFetchResolvesStorageViaAuthGet(t *testing.T) {
	storage, errMarshal := json.Marshal(zaiStorage{Version: storageVersion, Type: providerID, OAuthAccessToken: "tok-oauth"})
	if errMarshal != nil {
		t.Fatalf("marshal storage: %v", errMarshal)
	}
	var payload struct {
		AuthIndex      string `json:"auth_index"`
		Provider       string `json:"provider"`
		StorageJSON    []byte `json:"storage_json,omitempty"`
		HostCallbackID string `json:"host_callback_id"`
	}
	payload.AuthIndex = "idx-9"
	payload.Provider = providerID
	payload.HostCallbackID = "cb-9"
	raw, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		t.Fatalf("marshal payload: %v", errMarshal)
	}
	authGetCount := 0
	swapHostCaller(t, func(method string, req []byte) ([]byte, error) {
		if method == "host.auth.get" {
			authGetCount++
			var decoded struct {
				AuthIndex string `json:"auth_index"`
			}
			if errUnmarshal := json.Unmarshal(req, &decoded); errUnmarshal != nil {
				t.Errorf("decode host.auth.get request: %v", errUnmarshal)
			}
			if decoded.AuthIndex != "idx-9" {
				t.Errorf("host.auth.get auth_index = %q", decoded.AuthIndex)
			}
			return hostEnvelope(t, map[string]any{"auth_index": "idx-9", "name": "zai-test", "path": "auths/zai-test.json", "json": json.RawMessage(storage)}), nil
		}
		return upstreamHTTP(t, 200, []byte(`{"code":200,"success":true,"msg":"ok","data":{"limits":[{"unit":3,"number":5,"percentage":34.2,"nextResetTime":1770000000000}]}}`)), nil
	})
	result := decodeQuotaResult(t, callResult(t, pluginabi.MethodQuotaFetch, raw))
	if authGetCount != 1 {
		t.Fatalf("host.auth.get calls = %d, want 1", authGetCount)
	}
	buckets, ok := result["groups"]
	if !ok || !strings.Contains(string(buckets), "5h") {
		t.Fatalf("quota buckets missing after auth-index fallback: %s", buckets)
	}
}

// Acceptance: without storage_json and without auth_index, quota stays unknown.
func TestQuotaFetchWithoutCredentialsIsEmpty(t *testing.T) {
	calls := 0
	swapHostCaller(t, func(method string, payload []byte) ([]byte, error) {
		if method == "host.http.do" {
			calls++
		}
		return upstreamHTTP(t, 200, []byte(`{"code":200,"success":true,"data":{}}`)), nil
	})
	result := decodeQuotaResult(t, callResult(t, pluginabi.MethodQuotaFetch, []byte(`{"provider":"zai"}`)))
	if calls != 0 {
		t.Fatalf("upstream calls = %d, want 0", calls)
	}
	if len(result) != 0 {
		t.Fatalf("no-credential quota must stay unknown, got %s", result)
	}
}

// Acceptance: the quota/limit shape is only reported; string-encoded numbers
// must decode instead of silently emptying the response.
func TestQuotaFetchAcceptsStringNumbers(t *testing.T) {
	swapHostCaller(t, func(method string, payload []byte) ([]byte, error) {
		return upstreamHTTP(t, 200, []byte(`{"code":"200","success":true,"msg":"ok","data":{"limits":[{"unit":"3","number":"5","percentage":"34.2","nextResetTime":"1770000000000"}]}}`)), nil
	})
	result := decodeQuotaResult(t, callResult(t, pluginabi.MethodQuotaFetch, quotaFetchPayload(t)))
	buckets, ok := result["groups"]
	if !ok || !strings.Contains(string(buckets), "5h") {
		t.Fatalf("string-number quota body must decode to buckets, got %s", result)
	}
	var groups []struct {
		Buckets []struct {
			Window            string  `json:"window"`
			RemainingFraction float64 `json:"remainingFraction"`
		} `json:"buckets"`
	}
	if errUnmarshal := json.Unmarshal(buckets, &groups); errUnmarshal != nil || len(groups) == 0 || len(groups[0].Buckets) == 0 {
		t.Fatalf("decode buckets: %v %s", errUnmarshal, buckets)
	}
	if got := groups[0].Buckets[0].RemainingFraction; got < 0.6579 || got > 0.6581 {
		t.Fatalf("remainingFraction = %v, want ~0.658", got)
	}
}

// Acceptance: a 200 body whose envelope carries code 401 must advance to the
// next bearer candidate — live z.ai rejects the short-lived session token this
// way while the long-lived business token authorizes.
func TestQuotaFetchAdvancesOnEnvelope401(t *testing.T) {
	calls := 0
	swapHostCaller(t, func(method string, payload []byte) ([]byte, error) {
		calls++
		if calls == 1 {
			return upstreamHTTP(t, 200, []byte(`{"code":401,"msg":"token expired or incorrect","success":false}`)), nil
		}
		return upstreamHTTP(t, 200, []byte(`{"code":200,"success":true,"msg":"ok","data":{"limits":[{"unit":3,"number":5,"percentage":34.2}]}}`)), nil
	})
	result := decodeQuotaResult(t, callResult(t, pluginabi.MethodQuotaFetch, quotaFetchPayload(t)))
	if calls != 2 {
		t.Fatalf("upstream calls = %d, want 2", calls)
	}
	buckets, ok := result["groups"]
	if !ok || !strings.Contains(string(buckets), "5h") {
		t.Fatalf("envelope-401 must advance candidates, got %s", buckets)
	}
}
