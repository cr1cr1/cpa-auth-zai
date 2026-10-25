package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
)

// hostCall records one fake host callback invocation.
type hostCall struct {
	Method  string
	Payload []byte
}

// callLog records fake host calls; the stream pump goroutine also appends.
type callLog struct {
	mu    sync.Mutex
	calls []hostCall
}

func (l *callLog) add(call hostCall) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, call)
}

func (l *callLog) all() []hostCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]hostCall(nil), l.calls...)
}

func (l *callLog) count(method string) int {
	total := 0
	for _, call := range l.all() {
		if call.Method == method {
			total++
		}
	}
	return total
}

// testWithFakeHost swaps hostCaller for a scripted fake that wraps responses in
// a pluginabi envelope exactly like the real host bridge, and restores the real
// caller on cleanup. respond receives each call and its per-method index.
func testWithFakeHost(t *testing.T, respond func(call hostCall, index int) (any, error)) *callLog {
	t.Helper()
	log := &callLog{}
	original := hostCaller
	hostCaller = func(method string, payload []byte) ([]byte, error) {
		call := hostCall{Method: method, Payload: bytes.Clone(payload)}
		log.add(call)
		result, errRespond := respond(call, log.count(method)-1)
		if errRespond == nil && method == pluginabi.MethodHostHTTPStreamRead {
			// The real bridge blocks until a chunk or done; a fake that forgets
			// done must still terminate the pump, never spin forever.
			if readMap, ok := result.(map[string]any); ok {
				if _, hasDone := readMap["done"]; !hasDone {
					readMap["done"] = true
				}
			}
		}
		if errRespond != nil {
			return nil, errRespond
		}
		rawResult, errMarshal := json.Marshal(result)
		if errMarshal != nil {
			return nil, errMarshal
		}
		return json.Marshal(pluginabi.Envelope{OK: true, Result: rawResult})
	}
	t.Cleanup(func() { hostCaller = original })
	return log
}

func testJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, errMarshal := json.Marshal(value)
	if errMarshal != nil {
		t.Fatalf("marshal test value: %v", errMarshal)
	}
	return raw
}

// testCallOK invokes one RPC method and returns the unwrapped result JSON.
func testCallOK(t *testing.T, method string, payload []byte) json.RawMessage {
	t.Helper()
	raw, errHandle := handleCall(method, payload)
	if errHandle != nil {
		t.Fatalf("%s failed: %v", method, errHandle)
	}
	var envelope pluginabi.Envelope
	if errUnmarshal := json.Unmarshal(raw, &envelope); errUnmarshal != nil || !envelope.OK {
		t.Fatalf("%s envelope invalid: %s", method, raw)
	}
	return envelope.Result
}

// testCallErr invokes one RPC method and returns the typed plugin error.
func testCallErr(t *testing.T, method string, payload []byte) *pluginabi.Error {
	t.Helper()
	_, errHandle := handleCall(method, payload)
	if errHandle == nil {
		t.Fatalf("%s succeeded, want error", method)
	}
	var perr *pluginabi.Error
	if !errors.As(errHandle, &perr) {
		t.Fatalf("%s error %T does not carry a pluginabi.Error", method, errHandle)
	}
	return perr
}

func testDecode(t *testing.T, raw json.RawMessage, target any) {
	t.Helper()
	if errUnmarshal := json.Unmarshal(raw, target); errUnmarshal != nil {
		t.Fatalf("decode result %s: %v", raw, errUnmarshal)
	}
}

// Acceptance: the host-translated payload reaches the coding endpoint
// byte-for-byte with the first bearer candidate; nothing is rewritten.
func TestExecuteForwardsPayloadVerbatim(t *testing.T) {
	payload := []byte(`{"model":"glm-5.3","messages":[{"role":"user","content":"hi"}]}`)
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		return map[string]any{
			"StatusCode": 200,
			"Headers":    map[string][]string{"Content-Type": {"application/json"}},
			"Body":       []byte(`{"id":"up-1"}`),
		}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthID": "zai-test", "AuthProvider": "zai", "Model": "glm-5.3",
		"Stream":           false,
		"StorageJSON":      []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-oauth","biz_access_token":"tok-biz","zcode_jwt":"tok-zcode","api_key":"key-id.key-secret"}`),
		"Payload":          payload,
		"host_callback_id": "cb-1",
	})
	result := testCallOK(t, pluginabi.MethodExecutorExecute, request)

	if got := log.count(pluginabi.MethodHostHTTPDo); got != 1 {
		t.Fatalf("host.http.do calls = %d, want 1", got)
	}
	var doRequest struct {
		HostCallbackID string `json:"host_callback_id"`
		Request        struct {
			Method  string              `json:"method"`
			URL     string              `json:"url"`
			Headers map[string][]string `json:"headers"`
			Body    []byte              `json:"body"`
		} `json:"request"`
	}
	testDecode(t, log.all()[0].Payload, &doRequest)
	if doRequest.HostCallbackID != "cb-1" {
		t.Fatalf("host_callback_id = %q, want cb-1", doRequest.HostCallbackID)
	}
	if doRequest.Request.Method != "POST" || doRequest.Request.URL != codingChatCompletionsURL {
		t.Fatalf("upstream request = %s %s", doRequest.Request.Method, doRequest.Request.URL)
	}
	if !bytes.Equal(doRequest.Request.Body, payload) {
		t.Fatalf("payload mutated: %s", doRequest.Request.Body)
	}
	if got := doRequest.Request.Headers["Authorization"]; len(got) != 1 || got[0] != "Bearer key-id.key-secret" {
		t.Fatalf("Authorization = %v, want provisioned API key", got)
	}
	if got := doRequest.Request.Headers["X-Zcode-Token"]; len(got) != 1 || got[0] != "tok-zcode" {
		t.Fatalf("X-Zcode-Token = %v, want the login-derived ZCode JWT", got)
	}
	assertZCodeFingerprint(t, doRequest.Request.Headers, "4.0.39")

	var response struct {
		Payload  []byte              `json:"Payload"`
		Headers  map[string][]string `json:"Headers"`
		Metadata map[string]any      `json:"Metadata"`
	}
	testDecode(t, result, &response)
	if string(response.Payload) != `{"id":"up-1"}` {
		t.Fatalf("response payload = %s", response.Payload)
	}
	if got := response.Headers["Content-Type"]; len(got) != 1 || got[0] != "application/json" {
		t.Fatalf("response headers = %v", response.Headers)
	}
}

func TestExecuteRoutesNativeOpenAIResponses(t *testing.T) {
	payload := []byte(`{"model":"glm-5.3","input":"hello","stream":false}`)
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		var upstream hostHTTPRequest
		testDecode(t, call.Payload, &upstream)
		if upstream.Request.URL != "https://api.z.ai/api/v1/responses" {
			t.Errorf("upstream URL = %q, want native Responses endpoint", upstream.Request.URL)
		}
		if !bytes.Equal(upstream.Request.Body, payload) {
			t.Errorf("payload changed: %s", upstream.Request.Body)
		}
		assertZCodeFingerprint(t, upstream.Request.Headers, "4.0.39")
		return map[string]any{"StatusCode": 200, "Body": []byte(`{"id":"resp-1","object":"response","status":"completed","output":[]}`)}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider": "zai", "Format": "openai-response", "SourceFormat": "openai-response",
		"StorageJSON":      []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1","api_key":"key-id.key-secret"}`),
		"Payload":          payload,
		"host_callback_id": "cb-responses",
	})
	result := testCallOK(t, pluginabi.MethodExecutorExecute, request)
	var response struct {
		Payload []byte `json:"Payload"`
	}
	testDecode(t, result, &response)
	if !bytes.Equal(response.Payload, []byte(`{"id":"resp-1","object":"response","status":"completed","output":[]}`)) {
		t.Fatalf("response payload = %s", response.Payload)
	}
	if got := log.count(pluginabi.MethodHostHTTPDo); got != 1 {
		t.Fatalf("upstream calls = %d, want 1", got)
	}
}

func TestExecuteRoutesAnthropicMessages(t *testing.T) {
	payload := []byte(`{"model":"glm-5.3","max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`)
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		var upstream hostHTTPRequest
		testDecode(t, call.Payload, &upstream)
		if upstream.Request.URL != "https://api.z.ai/api/anthropic/v1/messages" {
			t.Errorf("upstream URL = %q, want native Anthropic Messages endpoint", upstream.Request.URL)
		}
		if got := upstream.Request.Headers["anthropic-version"]; len(got) != 1 || got[0] != "2023-06-01" {
			t.Errorf("anthropic-version = %v", got)
		}
		assertZCodeFingerprint(t, upstream.Request.Headers, "4.0.27")
		if !bytes.Equal(upstream.Request.Body, payload) {
			t.Errorf("payload changed: %s", upstream.Request.Body)
		}
		return map[string]any{"StatusCode": 200, "Body": []byte(`{"id":"msg-1","type":"message","role":"assistant","content":[]}`)}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider": "zai", "Format": "claude", "SourceFormat": "anthropic-messages",
		"StorageJSON":      []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1","api_key":"key-id.key-secret"}`),
		"Payload":          payload,
		"host_callback_id": "cb-anthropic",
	})
	result := testCallOK(t, pluginabi.MethodExecutorExecute, request)
	var response struct {
		Payload []byte `json:"Payload"`
	}
	testDecode(t, result, &response)
	if !bytes.Equal(response.Payload, []byte(`{"id":"msg-1","type":"message","role":"assistant","content":[]}`)) {
		t.Fatalf("response payload = %s", response.Payload)
	}
	if got := log.count(pluginabi.MethodHostHTTPDo); got != 1 {
		t.Fatalf("upstream calls = %d, want 1", got)
	}
}

func TestExecuteRoutesLegacyFormatMetadata(t *testing.T) {
	for _, tc := range []struct {
		name       string
		format     string
		wantURL    string
		wantHeader string
	}{
		{name: "responses", format: "openai-response", wantURL: codingResponsesURL},
		{name: "anthropic", format: "claude", wantURL: codingAnthropicURL, wantHeader: "2023-06-01"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testWithFakeHost(t, func(call hostCall, index int) (any, error) {
				var upstream hostHTTPRequest
				testDecode(t, call.Payload, &upstream)
				if upstream.Request.URL != tc.wantURL {
					t.Errorf("upstream URL = %q, want %q", upstream.Request.URL, tc.wantURL)
				}
				if tc.wantHeader != "" {
					if got := upstream.Request.Headers["anthropic-version"]; len(got) != 1 || got[0] != tc.wantHeader {
						t.Errorf("anthropic-version = %v", got)
					}
				}
				return map[string]any{"StatusCode": 200, "Body": []byte(`{"id":"native-1"}`)}, nil
			})
			request := testJSON(t, map[string]any{
				"AuthProvider": "zai", "Format": tc.format,
				"StorageJSON":      []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1","api_key":"key-id.key-secret"}`),
				"Payload":          []byte(`{"model":"glm-5.3"}`),
				"host_callback_id": "cb-legacy-format",
			})
			testCallOK(t, pluginabi.MethodExecutorExecute, request)
		})
	}
}

func TestExecuteRejectsMixedPayloadAndOutputFormats(t *testing.T) {
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		return map[string]any{"StatusCode": 200, "Body": []byte(`{"id":"chat-1"}`)}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider": "zai", "Format": "openai-response", "SourceFormat": "openai",
		"StorageJSON":      []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1","api_key":"key-id.key-secret"}`),
		"Payload":          []byte(`{"model":"glm-5.3","messages":[]}`),
		"host_callback_id": "cb-mixed-formats",
	})
	perr := testCallErr(t, pluginabi.MethodExecutorExecute, request)
	if perr.Code != "invalid_request" || perr.HTTPStatus != 400 {
		t.Fatalf("error = %+v, want invalid_request/400 for mixed formats", perr)
	}
	if got := log.count(pluginabi.MethodHostHTTPDo); got != 0 {
		t.Fatalf("upstream calls = %d, want 0", got)
	}
}

func TestExecuteRejectsUnsupportedFormat(t *testing.T) {
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		return map[string]any{"StatusCode": 200, "Body": []byte(`{}`)}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider": "zai", "Format": "gemini",
		"StorageJSON":      []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1","api_key":"key-id.key-secret"}`),
		"Payload":          []byte(`{"model":"glm-5.3"}`),
		"host_callback_id": "cb-unsupported",
	})
	perr := testCallErr(t, pluginabi.MethodExecutorExecute, request)
	if perr.HTTPStatus != 400 || !strings.Contains(perr.Message, "unsupported") {
		t.Fatalf("error = %+v, want unsupported format", perr)
	}
	if got := log.count(pluginabi.MethodHostHTTPDo); got != 0 {
		t.Fatalf("upstream calls = %d, want 0", got)
	}
}

func TestExecuteStreamRejectsUnsupportedFormat(t *testing.T) {
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		return map[string]any{}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider": "zai", "Stream": true, "Format": "gemini",
		"StorageJSON": []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1","api_key":"key-id.key-secret"}`),
		"Payload":     []byte(`{"model":"glm-5.3","stream":true}`),
		"stream_id":   "r-unsupported", "host_callback_id": "cb-unsupported-stream",
	})
	perr := testCallErr(t, pluginabi.MethodExecutorExecuteStream, request)
	if perr.Code != "invalid_request" || perr.HTTPStatus != 400 {
		t.Fatalf("error = %+v, want invalid_request/400", perr)
	}
	if got := log.count(pluginabi.MethodHostHTTPDoStream); got != 0 {
		t.Fatalf("upstream calls = %d, want 0", got)
	}
}

func TestCodingRequestHeadersOmitsBlankZcodeToken(t *testing.T) {
	for _, token := range []string{"", " \n"} {
		headers := codingRequestHeaders("tok-oauth", token)
		if _, exists := headers["X-Zcode-Token"]; exists {
			t.Fatalf("blank ZCode JWT %q must not produce X-Zcode-Token: %v", token, headers)
		}
	}
}

// Acceptance: inference never retries with login tokens when the API key is rejected.
func TestExecuteDoesNotFallBackToLoginTokens(t *testing.T) {
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		return map[string]any{"StatusCode": 401, "Body": []byte(`{"error":{"message":"expired"}}`)}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider": "zai", "Model": "glm-5.3",
		"StorageJSON":      []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1","api_key":"key-id.key-secret","biz_access_token":"tok-2"}`),
		"Payload":          []byte(`{"model":"glm-5.3"}`),
		"host_callback_id": "cb-1",
	})
	perr := testCallErr(t, pluginabi.MethodExecutorExecute, request)
	if perr.HTTPStatus != 401 {
		t.Fatalf("HTTPStatus = %d, want 401", perr.HTTPStatus)
	}
	if got := log.count(pluginabi.MethodHostHTTPDo); got != 1 {
		t.Fatalf("attempts = %d, want only the API-key request", got)
	}
}

// Acceptance: a final 401 surfaces as a plugin error carrying the upstream
// status so CPA maps it to the documented client error.
func TestExecuteSurfacesUpstreamStatus(t *testing.T) {
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		return map[string]any{"StatusCode": 401, "Body": []byte(`{"error":{"message":"invalid token"}}`)}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider":     "zai",
		"StorageJSON":      []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1","api_key":"key-id.key-secret"}`),
		"Payload":          []byte(`{"model":"glm-5.3"}`),
		"host_callback_id": "cb-1",
	})
	perr := testCallErr(t, pluginabi.MethodExecutorExecute, request)
	if perr.HTTPStatus != 401 {
		t.Fatalf("HTTPStatus = %d, want 401", perr.HTTPStatus)
	}
	if got := log.count(pluginabi.MethodHostHTTPDo); got != 1 {
		t.Fatalf("attempts = %d, want 1 for a single candidate", got)
	}
}

// Acceptance: the upstream business error detail is kept (sanitized) so quota
// and balance problems stay actionable without leaking credentials.
func TestExecuteUpstreamErrorDetailSanitized(t *testing.T) {
	testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		return map[string]any{"StatusCode": 403, "Body": []byte(`{"code":1113,"msg":"Insufficient Balance"}`)}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider":     "zai",
		"StorageJSON":      []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1","api_key":"key-id.key-secret"}`),
		"Payload":          []byte(`{"model":"glm-5.3"}`),
		"host_callback_id": "cb-1",
	})
	perr := testCallErr(t, pluginabi.MethodExecutorExecute, request)
	if perr.Code != "upstream_error" || perr.HTTPStatus != 403 {
		t.Fatalf("error = %+v, want upstream_error/403", perr)
	}
	if !strings.Contains(perr.Message, "Insufficient Balance") {
		t.Fatalf("message = %q, want upstream detail", perr.Message)
	}
	if strings.Contains(perr.Message, "tok-1") || strings.Contains(perr.Message, "Bearer") {
		t.Fatalf("message leaks credentials: %q", perr.Message)
	}
}

// Acceptance: the stream response is async (headers only, no chunks), upstream
// SSE bytes are emitted on the RPC stream id in order, and the stream closes
// cleanly. The HTTP stream id and the RPC stream id stay separate.
func TestExecuteStreamEmitsAndCloses(t *testing.T) {
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		switch call.Method {
		case pluginabi.MethodHostHTTPDoStream:
			var upstream hostHTTPRequest
			testDecode(t, call.Payload, &upstream)
			if got := upstream.Request.Headers["X-Zcode-Token"]; len(got) != 1 || got[0] != "tok-zcode" {
				t.Fatalf("X-Zcode-Token = %v, want the login-derived ZCode JWT", got)
			}
			if got := upstream.Request.Headers["Authorization"]; len(got) != 1 || got[0] != "Bearer key-id.key-secret" {
				t.Fatalf("Authorization = %v, want provisioned API key", got)
			}
			assertZCodeFingerprint(t, upstream.Request.Headers, "4.0.39")
			return map[string]any{"status_code": 200, "stream_id": "h1"}, nil
		case pluginabi.MethodHostHTTPStreamRead:
			switch index {
			case 0:
				return map[string]any{"payload": []byte("a"), "done": false}, nil
			case 1:
				return map[string]any{"payload": []byte("b"), "done": false}, nil
			default:
				return map[string]any{"done": true}, nil
			}
		}
		return map[string]any{}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider": "zai", "Stream": true,
		"StorageJSON": []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1","zcode_jwt":"tok-zcode","api_key":"key-id.key-secret"}`),
		"Payload":     []byte(`{"model":"glm-5.3","stream":true}`),
		"stream_id":   "r1", "host_callback_id": "cb-1",
	})
	result := testCallOK(t, pluginabi.MethodExecutorExecuteStream, request)

	var response map[string]json.RawMessage
	testDecode(t, result, &response)
	if _, ok := response["chunks"]; ok {
		t.Errorf("async response must not carry chunks: %s", result)
	}
	if _, ok := response["headers"]; !ok {
		t.Errorf("async response must carry headers: %s", result)
	}

	// Wait before asserting pump effects even on failure: a Fatalf here would
	// orphan the pump into the next test.
	streamPumps.Wait()

	type recordedEmit struct {
		StreamID string `json:"stream_id"`
		Payload  []byte `json:"payload"`
		Error    string `json:"error"`
	}
	var emits []recordedEmit
	var closes []struct {
		StreamID string `json:"stream_id"`
		Error    string `json:"error"`
	}
	for _, call := range log.all() {
		switch call.Method {
		case pluginabi.MethodHostStreamEmit:
			var emit recordedEmit
			testDecode(t, call.Payload, &emit)
			emits = append(emits, emit)
		case pluginabi.MethodHostStreamClose:
			var closeRequest struct {
				StreamID string `json:"stream_id"`
				Error    string `json:"error"`
			}
			testDecode(t, call.Payload, &closeRequest)
			closes = append(closes, closeRequest)
		case pluginabi.MethodHostHTTPStreamRead:
			var readRequest hostStreamID
			testDecode(t, call.Payload, &readRequest)
			if readRequest.StreamID != "h1" {
				t.Fatalf("stream_read used http stream id %q, want h1", readRequest.StreamID)
			}
		}
	}
	want := []recordedEmit{
		{StreamID: "r1", Payload: []byte("a")},
		{StreamID: "r1", Payload: []byte("b")},
	}
	if len(emits) != len(want) {
		t.Fatalf("emits = %d, want %d", len(emits), len(want))
	}
	for i := range want {
		if emits[i].StreamID != want[i].StreamID || !bytes.Equal(emits[i].Payload, want[i].Payload) || emits[i].Error != "" {
			t.Fatalf("emit %d = %+v, want rpc id r1 payload %q", i, emits[i], want[i].Payload)
		}
	}
	if len(closes) != 1 || closes[0].StreamID != "r1" || closes[0].Error != "" {
		t.Fatalf("closes = %+v, want one clean close on r1", closes)
	}
	if got := log.count(pluginabi.MethodHostHTTPStreamClose); got != 1 {
		t.Fatalf("HTTP stream closes = %d, want 1 on normal completion", got)
	}
}

// Acceptance: the streaming executor always asks z.ai for a stream, including
// when the input payload omits stream or explicitly disables it.
func TestExecuteStreamForcesUpstreamStreaming(t *testing.T) {
	tests := []struct {
		name    string
		format  string
		payload string
	}{
		{name: "chat completions omitted", format: "openai", payload: `{"model":"glm-5.3","messages":[]}`},
		{name: "openai responses omitted", format: "openai-response", payload: `{"model":"glm-5.3","input":"hello"}`},
		{name: "anthropic messages false", format: "claude", payload: `{"model":"glm-5.3","messages":[],"stream":false}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var upstreamPayload []byte
			testWithFakeHost(t, func(call hostCall, index int) (any, error) {
				switch call.Method {
				case pluginabi.MethodHostHTTPDoStream:
					var upstream hostHTTPRequest
					testDecode(t, call.Payload, &upstream)
					upstreamPayload = bytes.Clone(upstream.Request.Body)
					return map[string]any{"status_code": 200, "stream_id": "h-force-stream"}, nil
				case pluginabi.MethodHostHTTPStreamRead:
					return map[string]any{"done": true}, nil
				default:
					return map[string]any{}, nil
				}
			})
			request := testJSON(t, map[string]any{
				"AuthProvider": "zai", "Stream": true, "Format": tc.format, "SourceFormat": tc.format,
				"StorageJSON": []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-oauth","api_key":"key-id.key-secret"}`),
				"Payload":     []byte(tc.payload),
				"stream_id":   "r-force-stream", "host_callback_id": "cb-force-stream",
			})
			testCallOK(t, pluginabi.MethodExecutorExecuteStream, request)
			streamPumps.Wait()

			var payload map[string]json.RawMessage
			if errUnmarshal := json.Unmarshal(upstreamPayload, &payload); errUnmarshal != nil {
				t.Fatalf("upstream payload is not a JSON object: %s: %v", upstreamPayload, errUnmarshal)
			}
			if got := string(payload["stream"]); got != "true" {
				t.Fatalf("upstream stream = %s, want true; payload = %s", got, upstreamPayload)
			}
			delete(payload, "stream")
			var original map[string]json.RawMessage
			if errUnmarshal := json.Unmarshal([]byte(tc.payload), &original); errUnmarshal != nil {
				t.Fatalf("decode original payload: %v", errUnmarshal)
			}
			delete(original, "stream")
			gotFields, _ := json.Marshal(payload)
			wantFields, _ := json.Marshal(original)
			if !bytes.Equal(gotFields, wantFields) {
				t.Fatalf("other fields changed: got %s, want %s", gotFields, wantFields)
			}
		})
	}
}

func TestStreamingPayloadRequiresObject(t *testing.T) {
	for _, payload := range []string{"", "null", "[]"} {
		_, errStream := streamingPayload([]byte(payload))
		var perr *pluginabi.Error
		if !errors.As(errStream, &perr) || perr.Code != "invalid_request" || perr.HTTPStatus != 400 {
			t.Errorf("streamingPayload(%q) error = %v, want invalid_request/400", payload, errStream)
		}
	}
}

func TestExecuteStreamRoutesAnthropicMessagesAndPreservesTerminal(t *testing.T) {
	frame := []byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		switch call.Method {
		case pluginabi.MethodHostHTTPDoStream:
			var upstream hostHTTPRequest
			testDecode(t, call.Payload, &upstream)
			if upstream.Request.URL != "https://api.z.ai/api/anthropic/v1/messages" {
				t.Errorf("upstream URL = %q", upstream.Request.URL)
			}
			if got := upstream.Request.Headers["anthropic-version"]; len(got) != 1 || got[0] != "2023-06-01" {
				t.Errorf("anthropic-version = %v", got)
			}
			assertZCodeFingerprint(t, upstream.Request.Headers, "4.0.27")
			return map[string]any{"status_code": 200, "stream_id": "h-anthropic"}, nil
		case pluginabi.MethodHostHTTPStreamRead:
			return map[string]any{"payload": frame, "done": true}, nil
		}
		return map[string]any{}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider": "zai", "Stream": true, "Format": "claude", "SourceFormat": "claude",
		"StorageJSON": []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1","api_key":"key-id.key-secret"}`),
		"Payload":     []byte(`{"model":"glm-5.3","max_tokens":32,"messages":[],"stream":true}`),
		"stream_id":   "r-anthropic", "host_callback_id": "cb-anthropic-stream",
	})
	testCallOK(t, pluginabi.MethodExecutorExecuteStream, request)
	streamPumps.Wait()
	var emitted [][]byte
	for _, call := range log.all() {
		if call.Method == pluginabi.MethodHostStreamEmit {
			var emit rpcStreamEmit
			testDecode(t, call.Payload, &emit)
			emitted = append(emitted, emit.Payload)
		}
	}
	if len(emitted) != 1 || !bytes.Equal(emitted[0], frame) {
		t.Fatalf("emitted frames = %q, want unchanged message_stop", emitted)
	}
	if got := log.count(pluginabi.MethodHostHTTPStreamClose); got != 1 {
		t.Fatalf("HTTP stream closes = %d, want 1", got)
	}
}

func TestExecuteStreamRoutesNativeResponsesAndPreservesTerminal(t *testing.T) {
	payload := []byte(`{"model":"glm-5.3","input":"hello","stream":true}`)
	frames := [][]byte{
		[]byte("event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"message\"}}\n\n"),
		[]byte("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n"),
	}
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		switch call.Method {
		case pluginabi.MethodHostHTTPDoStream:
			var upstream hostHTTPRequest
			testDecode(t, call.Payload, &upstream)
			if upstream.Request.URL != "https://api.z.ai/api/v1/responses" || !bytes.Equal(upstream.Request.Body, payload) {
				t.Errorf("upstream request = %s %s", upstream.Request.URL, upstream.Request.Body)
			}
			return map[string]any{"status_code": 200, "stream_id": "h-native-responses"}, nil
		case pluginabi.MethodHostHTTPStreamRead:
			return map[string]any{"payload": frames[index], "done": index == len(frames)-1}, nil
		}
		return map[string]any{}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider": "zai", "Stream": true, "Format": "openai-response", "SourceFormat": "openai-response",
		"StorageJSON": []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1","api_key":"key-id.key-secret"}`),
		"Payload":     payload,
		"stream_id":   "r-native-responses", "host_callback_id": "cb-native-responses",
	})
	testCallOK(t, pluginabi.MethodExecutorExecuteStream, request)
	streamPumps.Wait()
	var emitted [][]byte
	for _, call := range log.all() {
		if call.Method == pluginabi.MethodHostStreamEmit {
			var emit rpcStreamEmit
			testDecode(t, call.Payload, &emit)
			if emit.Error != "" {
				t.Fatalf("stream emit error = %q", emit.Error)
			}
			emitted = append(emitted, emit.Payload)
		}
	}
	if len(emitted) != 2 || !bytes.Equal(emitted[0], frames[0]) || !bytes.Equal(emitted[1], frames[1]) {
		t.Fatalf("emitted frames = %q, want unchanged native terminal sequence", emitted)
	}
	if got := log.count(pluginabi.MethodHostHTTPStreamClose); got != 1 {
		t.Fatalf("HTTP stream closes = %d, want 1", got)
	}
}

func TestDebugMalformedSSELogsSanitizedPreview(t *testing.T) {
	malformed := `{"response":{"output":[{"arguments":"key-id.key-secret ` + strings.Repeat("x", 350) + "TAIL"
	splitAt := len(malformed) / 2
	frames := [][]byte{
		[]byte("event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n"),
		[]byte("event: error\ndata: " + malformed[:splitAt]),
		[]byte(malformed[splitAt:] + "\n\n"),
	}
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		switch call.Method {
		case pluginabi.MethodHostHTTPDoStream:
			return map[string]any{"status_code": 200, "stream_id": "h-debug-sse"}, nil
		case pluginabi.MethodHostHTTPStreamRead:
			return map[string]any{"payload": frames[index], "done": index == len(frames)-1}, nil
		}
		return map[string]any{}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider": "zai", "Stream": true, "Format": "openai-response", "SourceFormat": "openai-response",
		"StorageJSON": []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1","api_key":"key-id.key-secret"}`),
		"Payload":     []byte(`{"model":"glm-5.3","input":"hello","stream":true}`),
		"stream_id":   "r-debug-sse", "host_callback_id": "cb-debug-sse",
	})
	testCallOK(t, pluginabi.MethodExecutorExecuteStream, request)
	streamPumps.Wait()

	debugLogs := 0
	var emitted [][]byte
	for _, call := range log.all() {
		switch call.Method {
		case pluginabi.MethodHostStreamEmit:
			var emit rpcStreamEmit
			testDecode(t, call.Payload, &emit)
			emitted = append(emitted, emit.Payload)
		case pluginabi.MethodHostLog:
			var entry hostLogRequest
			testDecode(t, call.Payload, &entry)
			if entry.Message != "z.ai upstream SSE data is not valid JSON" {
				continue
			}
			debugLogs++
			if entry.Level != "debug" {
				t.Fatalf("log level = %q, want debug", entry.Level)
			}
			preview := fmt.Sprint(entry.Fields["data"])
			if strings.Contains(preview, "key-id.key-secret") || !strings.Contains(preview, "[redacted]") {
				t.Fatalf("debug preview = %q, want redacted malformed data", preview)
			}
			if got := fmt.Sprint(entry.Fields["data_length"]); got != fmt.Sprint(len(malformed)) {
				t.Fatalf("data_length = %s, want %d", got, len(malformed))
			}
			if got := fmt.Sprint(entry.Fields["parse_error"]); !strings.Contains(got, "unexpected end of JSON input") {
				t.Fatalf("parse_error = %q, want unexpected end", got)
			}
			if got := fmt.Sprint(entry.Fields["tail"]); !strings.Contains(got, "TAIL") {
				t.Fatalf("tail = %q, want trailing marker", got)
			}
		}
	}
	if debugLogs != 1 {
		t.Fatalf("debug logs = %d, want one malformed event", debugLogs)
	}
	if len(emitted) != len(frames) {
		t.Fatalf("emitted chunks = %d, want %d", len(emitted), len(frames))
	}
	for index := range frames {
		if !bytes.Equal(emitted[index], frames[index]) {
			t.Fatalf("chunk %d changed: %q", index, emitted[index])
		}
	}
}

// Acceptance: a rejection before the stream starts returns the upstream status,
// emits nothing, and releases the host HTTP stream.
func TestExecuteStreamUpstreamRejectsBeforeStream(t *testing.T) {
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		return map[string]any{"status_code": 401, "stream_id": "h9"}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider": "zai", "Stream": true,
		"StorageJSON": []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1","api_key":"key-id.key-secret"}`),
		"Payload":     []byte(`{"model":"glm-5.3","stream":true}`),
		"stream_id":   "r1",
	})
	perr := testCallErr(t, pluginabi.MethodExecutorExecuteStream, request)
	if perr.HTTPStatus != 401 {
		t.Errorf("HTTPStatus = %d, want 401", perr.HTTPStatus)
	}
	streamPumps.Wait()
	if got := log.count(pluginabi.MethodHostStreamEmit); got != 0 {
		t.Fatalf("emits = %d, want 0", got)
	}
	if got := log.count(pluginabi.MethodHostHTTPStreamRead); got != 0 {
		t.Fatalf("stream_read calls = %d, want 0", got)
	}
	var closed []hostStreamID
	for _, call := range log.all() {
		if call.Method == pluginabi.MethodHostHTTPStreamClose {
			var closeRequest hostStreamID
			testDecode(t, call.Payload, &closeRequest)
			closed = append(closed, closeRequest)
		}
	}
	if len(closed) != 1 || closed[0].StreamID != "h9" {
		t.Fatalf("stream_close = %+v, want one close of http stream h9", closed)
	}
}

// Acceptance: a mid-stream read error reaches the client as an emit with the
// error and the stream closes with the same message; no panic, no extra emits.
func TestExecuteStreamMidStreamError(t *testing.T) {
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		switch call.Method {
		case pluginabi.MethodHostHTTPDoStream:
			return map[string]any{"status_code": 200, "stream_id": "h1"}, nil
		case pluginabi.MethodHostHTTPStreamRead:
			return map[string]any{"error": "boom", "done": true}, nil
		}
		return map[string]any{}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider": "zai", "Stream": true,
		"StorageJSON": []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1","api_key":"key-id.key-secret"}`),
		"Payload":     []byte(`{"model":"glm-5.3","stream":true}`),
		"stream_id":   "r1",
	})
	if _, errHandle := handleCall(pluginabi.MethodExecutorExecuteStream, request); errHandle != nil {
		t.Fatalf("execute_stream failed: %v", errHandle)
	}
	streamPumps.Wait()

	emits := 0
	closes := 0
	for _, call := range log.all() {
		switch call.Method {
		case pluginabi.MethodHostStreamEmit:
			emits++
			var emit struct {
				StreamID string `json:"stream_id"`
				Error    string `json:"error"`
			}
			testDecode(t, call.Payload, &emit)
			if emit.StreamID != "r1" || emit.Error != "boom" {
				t.Fatalf("emit = %+v, want r1/boom", emit)
			}
		case pluginabi.MethodHostStreamClose:
			closes++
			var closeRequest struct {
				StreamID string `json:"stream_id"`
				Error    string `json:"error"`
			}
			testDecode(t, call.Payload, &closeRequest)
			if closeRequest.StreamID != "r1" || closeRequest.Error != "boom" {
				t.Fatalf("close = %+v, want r1/boom", closeRequest)
			}
		}
	}
	if emits != 1 || closes != 1 {
		t.Fatalf("emits = %d closes = %d, want 1/1", emits, closes)
	}
}

// Acceptance: token counting is unsupported and makes no upstream call.
func TestCountTokensUnsupported(t *testing.T) {
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		return map[string]any{}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider": "zai",
		"StorageJSON":  []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1","api_key":"key-id.key-secret"}`),
		"Payload":      []byte(`{"model":"glm-5.3"}`),
	})
	perr := testCallErr(t, pluginabi.MethodExecutorCountTokens, request)
	if perr.Code != "unsupported_operation" || perr.HTTPStatus != 501 {
		t.Fatalf("error = %+v, want unsupported_operation/501", perr)
	}
	if len(log.all()) != 0 {
		t.Fatalf("upstream calls = %d, want 0", len(log.all()))
	}
}

// Acceptance: executor-owned HTTP is restricted to the verified api.z.ai
// origin; other schemes and hosts are rejected before any host call.
func TestHTTPRequestRejectsForeignOrigin(t *testing.T) {
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		return map[string]any{}, nil
	})
	for _, target := range []string{"http://api.z.ai/api/monitor/x", "https://evil.com/api/x"} {
		request := testJSON(t, map[string]any{
			"Method": "GET", "URL": target, "host_callback_id": "cb-1",
		})
		perr := testCallErr(t, pluginabi.MethodExecutorHTTPRequest, request)
		if perr.Code != "forbidden_origin" || perr.HTTPStatus != 403 {
			t.Fatalf("%s: error = %+v, want forbidden_origin/403", target, perr)
		}
	}
	if len(log.all()) != 0 {
		t.Fatalf("upstream calls = %d, want 0", len(log.all()))
	}
}

// Acceptance: allowed api.z.ai requests pass through untouched and the
// executor response carries the upstream status, headers, and body as-is.
func TestHTTPRequestAllowsApiZai(t *testing.T) {
	requestHeaders := map[string][]string{"X-Test": {"1"}}
	requestBody := []byte(`{"q":1}`)
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		return map[string]any{
			"StatusCode": 200,
			"Headers":    map[string][]string{"X-Trace": {"t7"}},
			"Body":       []byte("ok"),
		}, nil
	})
	request := testJSON(t, map[string]any{
		"Method": "GET", "URL": "https://api.z.ai/api/biz/x",
		"Headers": requestHeaders, "Body": requestBody,
		"host_callback_id": "cb-1",
	})
	result := testCallOK(t, pluginabi.MethodExecutorHTTPRequest, request)

	var doRequest struct {
		HostCallbackID string `json:"host_callback_id"`
		Request        struct {
			Method  string              `json:"method"`
			URL     string              `json:"url"`
			Headers map[string][]string `json:"headers"`
			Body    []byte              `json:"body"`
		} `json:"request"`
	}
	testDecode(t, log.all()[0].Payload, &doRequest)
	if doRequest.HostCallbackID != "cb-1" || doRequest.Request.Method != "GET" ||
		doRequest.Request.URL != "https://api.z.ai/api/biz/x" {
		t.Fatalf("forwarded request = %+v", doRequest)
	}
	if len(doRequest.Request.Headers["X-Test"]) != 1 || doRequest.Request.Headers["X-Test"][0] != "1" {
		t.Fatalf("forwarded headers = %v", doRequest.Request.Headers)
	}
	if _, hasAuth := doRequest.Request.Headers["Authorization"]; hasAuth {
		t.Fatalf("credentials must not be attached to executor HTTP: %v", doRequest.Request.Headers)
	}
	if !bytes.Equal(doRequest.Request.Body, requestBody) {
		t.Fatalf("forwarded body = %s", doRequest.Request.Body)
	}

	var response struct {
		StatusCode int                 `json:"StatusCode"`
		Headers    map[string][]string `json:"Headers"`
		Body       []byte              `json:"Body"`
	}
	testDecode(t, result, &response)
	if response.StatusCode != 200 || string(response.Body) != "ok" {
		t.Fatalf("response = %d %s", response.StatusCode, response.Body)
	}
	if got := response.Headers["X-Trace"]; len(got) != 1 || got[0] != "t7" {
		t.Fatalf("response headers = %v", response.Headers)
	}
}

// Acceptance: the executor identifier is the provider key CPA routes by; it
// must equal the auth provider key so requests reach this executor.
func TestExecutorIdentifierMatchesProvider(t *testing.T) {
	result := testCallOK(t, pluginabi.MethodExecutorIdentifier, []byte("{}"))
	var response struct {
		Identifier string `json:"identifier"`
	}
	testDecode(t, result, &response)
	if response.Identifier != providerID {
		t.Fatalf("identifier = %q, want %q", response.Identifier, providerID)
	}
}

// Acceptance: streaming inference never retries with login tokens.
func TestExecuteStreamDoesNotFallBackToLoginTokens(t *testing.T) {
	doStreamCalls := 0
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		if call.Method == pluginabi.MethodHostHTTPDoStream {
			doStreamCalls++
			return map[string]any{"status_code": 401, "stream_id": "h-reject"}, nil
		}
		return map[string]any{}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider": "zai", "Stream": true,
		"StorageJSON": []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-a","api_key":"key-id.key-secret","biz_access_token":"tok-b"}`),
		"Payload":     []byte(`{"model":"glm-5.3","stream":true}`),
		"stream_id":   "r-fb", "host_callback_id": "cb-fb",
	})
	perr := testCallErr(t, pluginabi.MethodExecutorExecuteStream, request)
	streamPumps.Wait()
	if perr.HTTPStatus != 401 || doStreamCalls != 1 {
		t.Fatalf("error = %+v, do_stream calls = %d; want one API-key attempt", perr, doStreamCalls)
	}
	if log.count(pluginabi.MethodHostHTTPStreamClose) != 1 {
		t.Fatalf("rejected HTTP stream was not closed: %+v", log.all())
	}
}

// Acceptance: an emit failure must terminate the pump through closeRPCStream
// with the failure message, never spin or leak the stream.
func TestExecuteStreamEmitFailureCloses(t *testing.T) {
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		switch call.Method {
		case pluginabi.MethodHostHTTPDoStream:
			return map[string]any{"status_code": 200, "stream_id": "h-e"}, nil
		case pluginabi.MethodHostHTTPStreamRead:
			return map[string]any{"payload": []byte("data"), "done": false}, nil
		case pluginabi.MethodHostStreamEmit:
			return nil, fmt.Errorf("emit channel closed for key-id.key-secret")
		}
		return map[string]any{}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider": "zai", "Stream": true,
		"StorageJSON": []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1","api_key":"key-id.key-secret"}`),
		"Payload":     []byte(`{"model":"glm-5.3","stream":true}`),
		"stream_id":   "r-emit-fail", "host_callback_id": "cb-ef",
	})
	testCallOK(t, pluginabi.MethodExecutorExecuteStream, request)
	streamPumps.Wait()

	var closeErr string
	for _, call := range log.all() {
		if call.Method == pluginabi.MethodHostStreamClose {
			var closed struct {
				StreamID string `json:"stream_id"`
				Error    string `json:"error"`
			}
			testDecode(t, call.Payload, &closed)
			if closed.StreamID == "r-emit-fail" && closed.Error != "" {
				closeErr = closed.Error
			}
		}
	}
	if closeErr == "" || strings.Contains(closeErr, "key-id.key-secret") || !strings.Contains(closeErr, "[redacted]") {
		t.Fatalf("emit failure close error = %q, want redacted API key", closeErr)
	}
	if got := log.count(pluginabi.MethodHostHTTPStreamClose); got != 1 {
		t.Fatalf("HTTP stream closes = %d, want 1", got)
	}
}

func TestExecuteRequiresProvisionedAPIKey(t *testing.T) {
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		t.Errorf("request without an API key reached the host: %s", call.Method)
		return map[string]any{}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider":     "zai",
		"StorageJSON":      []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-a","biz_access_token":"tok-b"}`),
		"Payload":          []byte(`{"model":"glm-5.3"}`),
		"host_callback_id": "cb-env",
	})
	perr := testCallErr(t, pluginabi.MethodExecutorExecute, request)
	if perr.HTTPStatus != 400 || !strings.Contains(perr.Message, "sign in again") {
		t.Fatalf("error = %+v, want re-login guidance", perr)
	}
	if got := log.count(pluginabi.MethodHostHTTPDo); got != 0 {
		t.Fatalf("upstream calls = %d, want 0", got)
	}
}

// Acceptance: when every candidate is envelope-rejected, the executor
// surfaces HTTP 401 for CPA's client mapping.
func TestExecuteEnvelope401SurfacesUnauthorized(t *testing.T) {
	testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		if call.Method == pluginabi.MethodHostHTTPDo {
			return hostHTTPResponse{StatusCode: 200, Body: []byte(`{"code":401,"msg":"token expired or incorrect","success":false}`)}, nil
		}
		return map[string]any{}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider":     "zai",
		"StorageJSON":      []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-a","api_key":"key-id.key-secret"}`),
		"Payload":          []byte(`{"model":"glm-5.3"}`),
		"host_callback_id": "cb-env2",
	})
	_, errHandle := handleCall(pluginabi.MethodExecutorExecute, request)
	if errHandle == nil {
		t.Fatalf("all-rejected execute must fail")
	}
	type statusCoder interface{ StatusCode() int }
	var sc statusCoder
	if !errors.As(errHandle, &sc) || sc.StatusCode() != 401 {
		t.Fatalf("error should carry 401, got %v", errHandle)
	}
}

// Acceptance: an HTTP 200 business failure is a sanitized bad-gateway error,
// not a successful OpenAI response and not a bearer fallback.
func TestExecuteEnvelopeBusinessFailure(t *testing.T) {
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		return hostHTTPResponse{StatusCode: 200, Body: []byte(`{"code":1234,"msg":"Network error","success":false}`)}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider":     "zai",
		"StorageJSON":      []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-a","api_key":"key-id.key-secret","biz_access_token":"tok-b"}`),
		"Payload":          []byte(`{"model":"glm-5.3"}`),
		"host_callback_id": "cb-business",
	})
	perr := testCallErr(t, pluginabi.MethodExecutorExecute, request)
	if perr.Code != "upstream_error" || perr.HTTPStatus != 502 || perr.Message != "Network error" {
		t.Fatalf("error = %+v, want sanitized upstream_error/502 Network error", perr)
	}
	if got := log.count(pluginabi.MethodHostHTTPDo); got != 1 {
		t.Fatalf("upstream calls = %d, want 1 without fallback", got)
	}
}

// Acceptance: an envelope 401 closes the stream without login-token fallback.
func TestExecuteStreamEnvelope401IsTerminal(t *testing.T) {
	doStreamCalls := 0
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		switch call.Method {
		case pluginabi.MethodHostHTTPDoStream:
			doStreamCalls++
			return map[string]any{"status_code": 200, "stream_id": "h-envelope-reject"}, nil
		case pluginabi.MethodHostHTTPStreamRead:
			return map[string]any{"payload": []byte(`{"code":401,"msg":"token expired","success":false}`), "done": true}, nil
		}
		return map[string]any{}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider": "zai", "Stream": true,
		"StorageJSON": []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-a","api_key":"key-id.key-secret","biz_access_token":"tok-b","zcode_jwt":"tok-zcode"}`),
		"Payload":     []byte(`{"model":"glm-5.3","stream":true}`),
		"stream_id":   "r-envelope-terminal", "host_callback_id": "cb-envelope-terminal",
	})
	testCallOK(t, pluginabi.MethodExecutorExecuteStream, request)
	streamPumps.Wait()
	if doStreamCalls != 1 {
		t.Fatalf("do_stream calls = %d, want no login-token retry", doStreamCalls)
	}
	var closeError string
	for _, call := range log.all() {
		if call.Method == pluginabi.MethodHostStreamClose {
			var closed rpcStreamClose
			testDecode(t, call.Payload, &closed)
			closeError = closed.Error
		}
	}
	if closeError != "token expired" {
		t.Fatalf("RPC close error = %q, want token expired", closeError)
	}
}

// Acceptance: a terminal business envelope closes both streams with the
// sanitized upstream error; it is never emitted as data or closed as success.
func TestExecuteStreamTerminalBusinessEnvelope(t *testing.T) {
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		switch call.Method {
		case pluginabi.MethodHostHTTPDoStream:
			return map[string]any{"status_code": 200, "stream_id": "h-terminal"}, nil
		case pluginabi.MethodHostHTTPStreamRead:
			return map[string]any{"payload": []byte(`{"code":1234,"msg":"Network error","success":false}`), "done": true}, nil
		}
		return map[string]any{}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider": "zai", "Stream": true,
		"StorageJSON": []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-a","api_key":"key-id.key-secret"}`),
		"Payload":     []byte(`{"model":"glm-5.3","stream":true}`),
		"stream_id":   "r-terminal", "host_callback_id": "cb-terminal",
	})
	testCallOK(t, pluginabi.MethodExecutorExecuteStream, request)
	streamPumps.Wait()

	var emits []rpcStreamEmit
	var closes []rpcStreamClose
	closedHTTP := false
	for _, call := range log.all() {
		switch call.Method {
		case pluginabi.MethodHostStreamEmit:
			var emit rpcStreamEmit
			testDecode(t, call.Payload, &emit)
			emits = append(emits, emit)
		case pluginabi.MethodHostStreamClose:
			var closeRequest rpcStreamClose
			testDecode(t, call.Payload, &closeRequest)
			closes = append(closes, closeRequest)
		case pluginabi.MethodHostHTTPStreamClose:
			var closeRequest hostStreamID
			testDecode(t, call.Payload, &closeRequest)
			closedHTTP = closeRequest.StreamID == "h-terminal"
		}
	}
	if len(emits) != 1 || emits[0].Error != "Network error" || len(emits[0].Payload) != 0 {
		t.Fatalf("emits = %+v, want only Network error", emits)
	}
	if len(closes) != 1 || closes[0].Error != "Network error" {
		t.Fatalf("RPC closes = %+v, want one Network error close", closes)
	}
	if !closedHTTP {
		t.Fatalf("terminal HTTP stream was not closed")
	}
}

// Acceptance: arbitrary host chunking cannot leak fragments of an envelope 401.
func TestExecuteStreamFragmentedEnvelope401IsTerminal(t *testing.T) {
	doStreamCalls := 0
	fragmentIndex := 0
	fragments := [][]byte{
		[]byte(`{"code":40`),
		[]byte(`1,"msg":"token `),
		[]byte(`expired","success":false}`),
	}
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		switch call.Method {
		case pluginabi.MethodHostHTTPDoStream:
			doStreamCalls++
			return map[string]any{"status_code": 200, "stream_id": "h-fragmented"}, nil
		case pluginabi.MethodHostHTTPStreamRead:
			payload := fragments[fragmentIndex]
			fragmentIndex++
			return map[string]any{"payload": payload, "done": fragmentIndex == len(fragments)}, nil
		}
		return map[string]any{}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider": "zai", "Stream": true,
		"StorageJSON": []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-a","api_key":"key-id.key-secret","biz_access_token":"tok-b"}`),
		"Payload":     []byte(`{"model":"glm-5.3","stream":true}`),
		"stream_id":   "r-fragmented", "host_callback_id": "cb-fragmented",
	})
	testCallOK(t, pluginabi.MethodExecutorExecuteStream, request)
	streamPumps.Wait()
	if doStreamCalls != 1 {
		t.Fatalf("do_stream calls = %d, want no login-token retry", doStreamCalls)
	}
	var closeError string
	for _, call := range log.all() {
		if call.Method == pluginabi.MethodHostStreamClose {
			var closed rpcStreamClose
			testDecode(t, call.Payload, &closed)
			closeError = closed.Error
		}
	}
	if closeError != "token expired" {
		t.Fatalf("RPC close error = %q, want token expired", closeError)
	}
}

// Acceptance: bearer fallback is safe only before client output. A later
// envelope 401 closes the existing RPC stream with an error without retrying.
func TestExecuteStreamDoesNotFallBackAfterOutput(t *testing.T) {
	doStreamCalls := 0
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		switch call.Method {
		case pluginabi.MethodHostHTTPDoStream:
			doStreamCalls++
			return map[string]any{"status_code": 200, "stream_id": "h-started"}, nil
		case pluginabi.MethodHostHTTPStreamRead:
			if index == 0 {
				return map[string]any{"payload": []byte("data: started\n\n"), "done": false}, nil
			}
			return map[string]any{"payload": []byte(`{"code":401,"msg":"token expired","success":false}`), "done": true}, nil
		}
		return map[string]any{}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider": "zai", "Stream": true,
		"StorageJSON": []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-a","api_key":"key-id.key-secret","biz_access_token":"tok-b"}`),
		"Payload":     []byte(`{"model":"glm-5.3","stream":true}`),
		"stream_id":   "r-started", "host_callback_id": "cb-started",
	})
	testCallOK(t, pluginabi.MethodExecutorExecuteStream, request)
	streamPumps.Wait()

	if doStreamCalls != 1 {
		t.Fatalf("do_stream calls = %d, want no retry after output", doStreamCalls)
	}
	var emittedData [][]byte
	var closeError string
	for _, call := range log.all() {
		switch call.Method {
		case pluginabi.MethodHostStreamEmit:
			var emit rpcStreamEmit
			testDecode(t, call.Payload, &emit)
			if len(emit.Payload) > 0 {
				emittedData = append(emittedData, emit.Payload)
			}
		case pluginabi.MethodHostStreamClose:
			var closeRequest rpcStreamClose
			testDecode(t, call.Payload, &closeRequest)
			closeError = closeRequest.Error
		}
	}
	if len(emittedData) != 1 || !bytes.Equal(emittedData[0], []byte("data: started\n\n")) {
		t.Fatalf("emitted data = %q, want only initial SSE bytes", emittedData)
	}
	if closeError != "token expired" {
		t.Fatalf("RPC close error = %q, want token expired", closeError)
	}
}
