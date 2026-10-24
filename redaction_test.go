package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
)

// Acceptance: the private hosted-flow poll token is never returned when the
// upstream login endpoint echoes it in an error message.
func TestLoginStartRedactsPollToken(t *testing.T) {
	resetAuthFlows()
	t.Cleanup(resetAuthFlows)
	var pollToken string
	fake := installAuthFakeHost(t, func(call authHostCall) []byte {
		pollToken = strings.TrimPrefix(call.Headers["Authorization"][0], "Bearer ")
		return authHostEnvelope(401, `{"code":401,"msg":"Bearer `+pollToken+` rejected"}`)
	})
	_, errHandle := handleCall(pluginabi.MethodAuthLoginStart, []byte(`{"Provider":"zai","host_callback_id":"cb-1"}`))
	if errHandle == nil {
		t.Fatalf("login start must fail")
	}
	if pollToken == "" {
		t.Fatalf("fake host did not capture the poll token")
	}
	if strings.Contains(errHandle.Error(), pollToken) || !strings.Contains(errHandle.Error(), "[redacted]") {
		t.Fatalf("login error did not redact the poll token: %q", errHandle)
	}
	logs := fake.recordedLogs()
	raw, _ := json.Marshal(logs)
	if len(logs) != 1 || logs[0].Level != "error" || strings.Contains(string(raw), pollToken) || !strings.Contains(string(raw), "[redacted]") {
		t.Fatalf("login failure log did not redact the poll token: %s", raw)
	}
}

// Acceptance: an executor upstream error cannot echo any rejected bearer
// candidate back to the API client.
func TestExecuteRedactsRejectedCandidate(t *testing.T) {
	testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		return map[string]any{
			"StatusCode": 401,
			"Body":       []byte(`{"error":{"message":"Bearer key-id.key-secret rejected"}}`),
		}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider":     "zai",
		"StorageJSON":      []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1","api_key":"key-id.key-secret"}`),
		"Payload":          []byte(`{"model":"glm-5.3"}`),
		"host_callback_id": "cb-1",
	})
	perr := testCallErr(t, pluginabi.MethodExecutorExecute, request)
	if strings.Contains(perr.Message, "key-id.key-secret") || !strings.Contains(perr.Message, "[redacted]") {
		t.Fatalf("executor error did not redact the candidate: %q", perr.Message)
	}
}

// Acceptance: usage-history business errors cross the management route with
// credentials redacted, not only when the decoder is called directly.
func TestHistoryRedactsUpstreamCredential(t *testing.T) {
	storageJSON := []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-oauth"}`)
	var logs []capturedHostLog
	swapHostCallerWithLogs(t, &logs, func(method string, payload []byte) ([]byte, error) {
		switch method {
		case pluginabi.MethodHostAuthGet:
			return hostEnvelope(t, map[string]any{"auth_index": "idx-1", "json": json.RawMessage(storageJSON)}), nil
		case pluginabi.MethodHostHTTPDo:
			return upstreamHTTP(t, 200, []byte(`{"code":1234,"msg":"tok-oauth rejected","success":false}`)), nil
		}
		t.Fatalf("unexpected host call %s", method)
		return nil, nil
	})
	raw, errHandle := handleCall(pluginabi.MethodManagementHandle, mgmtHistoryPayload(t, map[string][]string{
		"auth_index": {"idx-1"},
		"start":      {"2026-10-17T00:00:00+08:00"},
		"end":        {"2026-10-18T00:00:00+08:00"},
	}))
	if errHandle != nil {
		t.Fatalf("management.handle failed: %v", errHandle)
	}
	status, body := mgmtResult(t, raw)
	if status != 502 || strings.Contains(string(body), "tok-oauth") || !strings.Contains(string(body), "[redacted]") {
		t.Fatalf("history response did not redact the credential: status=%d body=%s", status, body)
	}
	rawLogs, _ := json.Marshal(logs)
	if len(logs) != 1 || logs[0].Level != "warn" || strings.Contains(string(rawLogs), "tok-oauth") || !strings.Contains(string(rawLogs), "[redacted]") {
		t.Fatalf("history failure log did not redact the credential: %s", rawLogs)
	}
}

func TestExecuteRedactsHostError(t *testing.T) {
	testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		return nil, fmt.Errorf("Bearer key-id.key-secret request failed")
	})
	request := testJSON(t, map[string]any{
		"AuthProvider":     "zai",
		"StorageJSON":      []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1","api_key":"key-id.key-secret"}`),
		"Payload":          []byte(`{"model":"glm-5.3"}`),
		"host_callback_id": "cb-1",
	})
	_, errHandle := handleCall(pluginabi.MethodExecutorExecute, request)
	if errHandle == nil || strings.Contains(errHandle.Error(), "key-id.key-secret") || !strings.Contains(errHandle.Error(), "[redacted]") {
		t.Fatalf("host error = %v, want redacted API key", errHandle)
	}
}

func TestExecuteStreamRedactsHostOpenError(t *testing.T) {
	testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		return nil, fmt.Errorf("Bearer key-id.key-secret open failed")
	})
	request := testJSON(t, map[string]any{
		"AuthProvider": "zai", "Stream": true,
		"StorageJSON": []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-a","api_key":"key-id.key-secret"}`),
		"Payload":     []byte(`{"model":"glm-5.3","stream":true}`),
		"stream_id":   "r-open-error", "host_callback_id": "cb-open-error",
	})
	_, errHandle := handleCall(pluginabi.MethodExecutorExecuteStream, request)
	if errHandle == nil || strings.Contains(errHandle.Error(), "key-id.key-secret") || !strings.Contains(errHandle.Error(), "[redacted]") {
		t.Fatalf("host open error = %v, want redacted API key", errHandle)
	}
}

func TestExecuteStreamRedactsHostReadError(t *testing.T) {
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		switch call.Method {
		case pluginabi.MethodHostHTTPDoStream:
			return map[string]any{"status_code": 200, "stream_id": "h-read-error"}, nil
		case pluginabi.MethodHostHTTPStreamRead:
			return map[string]any{"error": "Bearer key-id.key-secret read failed", "done": true}, nil
		}
		return map[string]any{}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider": "zai", "Stream": true,
		"StorageJSON": []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-a","api_key":"key-id.key-secret"}`),
		"Payload":     []byte(`{"model":"glm-5.3","stream":true}`),
		"stream_id":   "r-read-error", "host_callback_id": "cb-read-error",
	})
	testCallOK(t, pluginabi.MethodExecutorExecuteStream, request)
	streamPumps.Wait()
	for _, call := range log.all() {
		if call.Method != pluginabi.MethodHostStreamEmit && call.Method != pluginabi.MethodHostStreamClose {
			continue
		}
		message := string(call.Payload)
		if strings.Contains(message, "key-id.key-secret") || !strings.Contains(message, "[redacted]") {
			t.Fatalf("stream read error did not redact the API key: %s", message)
		}
	}
}

// Acceptance: terminal stream errors also redact the rejected candidate in
// both the emitted error and the RPC close error.
func TestExecuteStreamRedactsTerminalCandidate(t *testing.T) {
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		switch call.Method {
		case pluginabi.MethodHostHTTPDoStream:
			return map[string]any{"status_code": 200, "stream_id": "h-redact"}, nil
		case pluginabi.MethodHostHTTPStreamRead:
			return map[string]any{"payload": []byte(`{"code":1234,"msg":"key-id.key-secret rejected","success":false}`), "done": true}, nil
		}
		return map[string]any{}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider": "zai", "Stream": true,
		"StorageJSON": []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-a","api_key":"key-id.key-secret"}`),
		"Payload":     []byte(`{"model":"glm-5.3","stream":true}`),
		"stream_id":   "r-redact", "host_callback_id": "cb-redact",
	})
	testCallOK(t, pluginabi.MethodExecutorExecuteStream, request)
	streamPumps.Wait()

	errors := 0
	for _, call := range log.all() {
		if call.Method != pluginabi.MethodHostStreamEmit && call.Method != pluginabi.MethodHostStreamClose {
			continue
		}
		message := string(call.Payload)
		if strings.Contains(message, "Network error") || strings.Contains(message, "rejected") || strings.Contains(message, "[redacted]") {
			errors++
		}
		if strings.Contains(message, "key-id.key-secret") || !strings.Contains(message, "[redacted]") {
			t.Fatalf("stream callback did not redact the candidate: %s", message)
		}
	}
	if errors != 2 {
		t.Fatalf("stream error callbacks = %d, want emit and close", errors)
	}
}
