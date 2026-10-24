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
		"StorageJSON":      []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-oauth","biz_access_token":"tok-biz"}`),
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
	if got := doRequest.Request.Headers["Authorization"]; len(got) != 1 || got[0] != "Bearer tok-oauth" {
		t.Fatalf("Authorization = %v, want first candidate", got)
	}

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

// Acceptance: a 401 from the first bearer candidate falls back to the next
// stored token, and the client sees the successful body.
func TestExecuteFallsBackOn401(t *testing.T) {
	upstreamBody := []byte(`{"id":"ok"}`)
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		if index == 0 {
			return map[string]any{"StatusCode": 401, "Body": []byte(`{"error":{"message":"expired"}}`)}, nil
		}
		return map[string]any{
			"StatusCode": 200,
			"Headers":    map[string][]string{"Content-Type": {"application/json"}},
			"Body":       upstreamBody,
		}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider": "zai", "Model": "glm-5.3",
		"StorageJSON":      []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1","biz_access_token":"tok-2"}`),
		"Payload":          []byte(`{"model":"glm-5.3"}`),
		"host_callback_id": "cb-1",
	})
	result := testCallOK(t, pluginabi.MethodExecutorExecute, request)

	if got := log.count(pluginabi.MethodHostHTTPDo); got != 2 {
		t.Fatalf("attempts = %d, want 2", got)
	}
	var second struct {
		Request struct {
			Headers map[string][]string `json:"headers"`
		} `json:"request"`
	}
	testDecode(t, log.all()[1].Payload, &second)
	if got := second.Request.Headers["Authorization"]; len(got) != 1 || got[0] != "Bearer tok-2" {
		t.Fatalf("second attempt Authorization = %v, want second candidate", got)
	}
	var response struct {
		Payload []byte `json:"Payload"`
	}
	testDecode(t, result, &response)
	if !bytes.Equal(response.Payload, upstreamBody) {
		t.Fatalf("response payload = %s, want upstream body", response.Payload)
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
		"StorageJSON":      []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1"}`),
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
		"StorageJSON":      []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1"}`),
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
		"StorageJSON": []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1"}`),
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
}

// Acceptance: a rejection before the stream starts returns the upstream status,
// emits nothing, and releases the host HTTP stream.
func TestExecuteStreamUpstreamRejectsBeforeStream(t *testing.T) {
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		return map[string]any{"status_code": 401, "stream_id": "h9"}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider": "zai", "Stream": true,
		"StorageJSON": []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1"}`),
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
		"StorageJSON": []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1"}`),
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
		"StorageJSON":  []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1"}`),
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

// Acceptance: a 401 on the streaming open closes that host HTTP stream and
// retries with the next bearer candidate; the retry's stream is the one pumped.
func TestExecuteStreamBearerFallbackOn401(t *testing.T) {
	doStreamCalls := 0
	log := testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		switch call.Method {
		case pluginabi.MethodHostHTTPDoStream:
			doStreamCalls++
			if doStreamCalls == 1 {
				return map[string]any{"status_code": 401, "stream_id": "h-reject"}, nil
			}
			return map[string]any{"status_code": 200, "stream_id": "h-ok"}, nil
		case pluginabi.MethodHostHTTPStreamRead:
			return map[string]any{"payload": []byte("chunk"), "done": true}, nil
		}
		return map[string]any{}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider": "zai", "Stream": true,
		"StorageJSON": []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-a","biz_access_token":"tok-b"}`),
		"Payload":     []byte(`{"model":"glm-5.3","stream":true}`),
		"stream_id":   "r-fb", "host_callback_id": "cb-fb",
	})
	testCallOK(t, pluginabi.MethodExecutorExecuteStream, request)
	streamPumps.Wait()

	if doStreamCalls != 2 {
		t.Fatalf("do_stream calls = %d, want 2 (fallback)", doStreamCalls)
	}
	closedRejected := false
	pumpedOK := false
	for _, call := range log.all() {
		if call.Method == pluginabi.MethodHostHTTPStreamClose {
			var closed struct {
				StreamID string `json:"stream_id"`
			}
			testDecode(t, call.Payload, &closed)
			if closed.StreamID == "h-reject" {
				closedRejected = true
			}
		}
		if call.Method == pluginabi.MethodHostHTTPStreamRead {
			var read struct {
				StreamID string `json:"stream_id"`
			}
			testDecode(t, call.Payload, &read)
			if read.StreamID == "h-ok" {
				pumpedOK = true
			}
		}
	}
	if !closedRejected {
		t.Fatalf("rejected 401 host stream was not closed")
	}
	if !pumpedOK {
		t.Fatalf("fallback host stream was not pumped")
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
			return nil, fmt.Errorf("emit channel closed")
		}
		return map[string]any{}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider": "zai", "Stream": true,
		"StorageJSON": []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1"}`),
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
	if closeErr == "" {
		t.Fatalf("emit failure did not close the rpc stream with an error")
	}
}

// Acceptance: the coding endpoint can wrap auth rejection in HTTP 200; the
// executor must advance candidates and only then surface the 401.
func TestExecuteAdvancesOnEnvelope401(t *testing.T) {
	doCalls := 0
	testWithFakeHost(t, func(call hostCall, index int) (any, error) {
		if call.Method == pluginabi.MethodHostHTTPDo {
			doCalls++
			if doCalls == 1 {
				return hostHTTPResponse{StatusCode: 200, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: []byte(`{"code":401,"msg":"token expired or incorrect","success":false}`)}, nil
			}
			return hostHTTPResponse{StatusCode: 200, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: []byte(`{"id":"x","choices":[]}`)}, nil
		}
		return map[string]any{}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthProvider":     "zai",
		"StorageJSON":      []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-a","biz_access_token":"tok-b"}`),
		"Payload":          []byte(`{"model":"glm-5.3"}`),
		"host_callback_id": "cb-env",
	})
	raw, errHandle := handleCall(pluginabi.MethodExecutorExecute, request)
	if errHandle != nil {
		t.Fatalf("second candidate should succeed: %v (%s)", errHandle, raw)
	}
	if doCalls != 2 {
		t.Fatalf("upstream calls = %d, want 2", doCalls)
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
		"StorageJSON":      []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-a"}`),
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
