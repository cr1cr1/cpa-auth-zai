package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
)

// hostCaller issues one host callback RPC. main.go installs the C ABI
// implementation at plugin init; tests replace it with a fake.
var hostCaller = func(method string, payload []byte) ([]byte, error) {
	return nil, fmt.Errorf("host callback %s is unavailable before plugin init", method)
}

func installHostCaller(fn func(method string, payload []byte) ([]byte, error)) {
	hostCaller = fn
}

// shutdown runs on plugin unload. The host bridge closes open streams, which
// ends every async stream pump; waiting here makes the unload clean.
func shutdown() {
	streamPumps.Wait()
}

// callHost performs one host callback and decodes the pluginabi envelope.
func callHost(method string, payload any) (json.RawMessage, error) {
	raw, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		return nil, fmt.Errorf("marshal host request %s: %w", method, errMarshal)
	}
	respRaw, errCall := hostCaller(method, raw)
	if errCall != nil {
		return nil, errCall
	}
	var envelope pluginabi.Envelope
	if errUnmarshal := json.Unmarshal(respRaw, &envelope); errUnmarshal != nil {
		return nil, fmt.Errorf("decode host envelope %s: %w", method, errUnmarshal)
	}
	if !envelope.OK {
		message := method
		if envelope.Error != nil {
			message = envelope.Error.Message
		}
		return nil, fmt.Errorf("host callback %s failed: %s", method, message)
	}
	return envelope.Result, nil
}

// hostHTTPRequest is the wire shape of a host.http.do request. The host
// response decodes into pluginapi.HTTPResponse-shaped JSON.
type hostHTTPRequest struct {
	HostCallbackID string              `json:"host_callback_id,omitempty"`
	OperationID    string              `json:"operation_id,omitempty"`
	Request        hostHTTPRequestSpec `json:"request,omitempty"`
}

type hostHTTPRequestSpec struct {
	Method  string              `json:"method,omitempty"`
	URL     string              `json:"url,omitempty"`
	Headers map[string][]string `json:"headers,omitempty"`
	Body    []byte              `json:"body,omitempty"`
}

type hostHTTPResponse struct {
	StatusCode int                 `json:"StatusCode"`
	Headers    map[string][]string `json:"Headers,omitempty"`
	Body       []byte              `json:"Body"`
}

// hostDo performs one upstream HTTP request through the host transport so
// proxy policy and request logging apply. The caller owns Authorization.
func hostDo(callbackID, method, url string, headers map[string][]string, body []byte) (*hostHTTPResponse, error) {
	if callbackID == "" {
		return nil, fmt.Errorf("host callback id is required for upstream requests")
	}
	payload := hostHTTPRequest{HostCallbackID: callbackID, Request: hostHTTPRequestSpec{
		Method: method, URL: url, Headers: headers, Body: body,
	}}
	result, errCall := callHost(pluginabi.MethodHostHTTPDo, payload)
	if errCall != nil {
		return nil, errCall
	}
	var resp hostHTTPResponse
	if errUnmarshal := json.Unmarshal(result, &resp); errUnmarshal != nil {
		return nil, fmt.Errorf("decode host HTTP response: %w", errUnmarshal)
	}
	return &resp, nil
}

// upstreamError converts a non-2xx upstream response into an error that carries
// the upstream status and a sanitized message. Known credentials are redacted
// before a short body prefix is kept; response headers are never copied.
func upstreamError(status int, body []byte, secrets ...string) error {
	detail := upstreamErrorDetail(body, secrets...)
	if detail == "" {
		return fmt.Errorf("upstream returned status %d", status)
	}
	return fmt.Errorf("upstream returned status %d: %s", status, detail)
}

func upstreamErrorDetail(body []byte, secrets ...string) string {
	var parsed struct {
		Msg     string `json:"msg"`
		Message string `json:"message"`
		Error   *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if errUnmarshal := json.Unmarshal(bytes.TrimSpace(body), &parsed); errUnmarshal == nil {
		switch {
		case parsed.Msg != "":
			return sanitizeDetail(redactSecrets(parsed.Msg, secrets))
		case parsed.Error != nil && parsed.Error.Message != "":
			return sanitizeDetail(redactSecrets(parsed.Error.Message, secrets))
		case parsed.Message != "":
			return sanitizeDetail(redactSecrets(parsed.Message, secrets))
		}
	}
	return sanitizeDetail(redactSecrets(string(body), secrets))
}

func redactSecrets(detail string, secrets []string) string {
	for _, secret := range secrets {
		if secret = strings.TrimSpace(secret); secret != "" {
			detail = strings.ReplaceAll(detail, secret, "[redacted]")
		}
	}
	return detail
}

func sanitizeDetail(detail string) string {
	detail = strings.TrimSpace(strings.Join(strings.Fields(detail), " "))
	if len(detail) > 300 {
		return detail[:300]
	}
	return detail
}
