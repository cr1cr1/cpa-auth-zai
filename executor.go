package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
)

func init() {
	registerHandler(pluginabi.MethodExecutorExecute, handleExecute)
	registerHandler(pluginabi.MethodExecutorExecuteStream, handleExecuteStream)
	registerHandler(pluginabi.MethodExecutorCountTokens, handleCountTokens)
	registerHandler(pluginabi.MethodExecutorHTTPRequest, handleExecutorHTTP)
}

// executorRequest decodes the executor.execute/execute_stream wire payload:
// the pluginapi.ExecutorRequest fields (JSON keys are the Go field names) plus
// the host extras stream_id and host_callback_id.
type executorRequest struct {
	AuthID          string              `json:"AuthID"`
	AuthProvider    string              `json:"AuthProvider"`
	Model           string              `json:"Model"`
	Format          string              `json:"Format"`
	Stream          bool                `json:"Stream"`
	Alt             string              `json:"Alt"`
	Headers         map[string][]string `json:"Headers"`
	Query           map[string][]string `json:"Query"`
	OriginalRequest []byte              `json:"OriginalRequest"`
	SourceFormat    string              `json:"SourceFormat"`
	Payload         []byte              `json:"Payload"`
	Metadata        map[string]any      `json:"Metadata"`
	StorageJSON     []byte              `json:"StorageJSON"`
	AuthMetadata    map[string]any      `json:"AuthMetadata"`
	AuthAttributes  map[string]string   `json:"AuthAttributes"`
	StreamID        string              `json:"stream_id"`
	HostCallbackID  string              `json:"host_callback_id"`
}

// executorResponse mirrors the pluginapi.ExecutorResponse wire keys.
type executorResponse struct {
	Payload  []byte              `json:"Payload"`
	Headers  map[string][]string `json:"Headers,omitempty"`
	Metadata map[string]any      `json:"Metadata,omitempty"`
}

// executorStreamResponse mirrors the host rpcExecutorStreamResponse wire keys.
// The async reply carries headers only; chunks are pushed via host.stream.emit.
type executorStreamResponse struct {
	Headers map[string][]string `json:"headers,omitempty"`
}

// executorHTTPRequest decodes the executor.http_request payload fields the
// handler forwards.
type executorHTTPRequest struct {
	Method         string              `json:"Method"`
	URL            string              `json:"URL"`
	Headers        map[string][]string `json:"Headers"`
	Body           []byte              `json:"Body"`
	HostCallbackID string              `json:"host_callback_id"`
}

// hostHTTPStreamStart decodes the host.http.do_stream response.
type hostHTTPStreamStart struct {
	StatusCode int                 `json:"status_code"`
	Headers    map[string][]string `json:"headers"`
	StreamID   string              `json:"stream_id"`
}

// hostHTTPStreamRead decodes one host.http.stream_read response.
type hostHTTPStreamRead struct {
	Payload []byte `json:"payload"`
	Error   string `json:"error"`
	Done    bool   `json:"done"`
}

// hostStreamID is the request shape of host.http.stream_read/stream_close.
type hostStreamID struct {
	StreamID string `json:"stream_id"`
}

// rpcStreamEmit and rpcStreamClose mirror the host stream bridge request keys
// (internal/pluginhost/stream_bridge.go).
type rpcStreamEmit struct {
	StreamID string `json:"stream_id"`
	Payload  []byte `json:"payload,omitempty"`
	Error    string `json:"error,omitempty"`
}

type rpcStreamClose struct {
	StreamID string `json:"stream_id"`
	Error    string `json:"error,omitempty"`
}

// streamPumps tracks the async stream pump goroutines so tests wait for them
// deterministically. Plugin shutdown closes the host bridge, which ends every
// pump through a failed host.http.stream_read; pumps cannot outlive it.
var streamPumps sync.WaitGroup

func decodeExecutorRequest(payload []byte) (*executorRequest, error) {
	var request executorRequest
	if errUnmarshal := json.Unmarshal(payload, &request); errUnmarshal != nil {
		return nil, fmt.Errorf("zai: decode executor request: %w", errUnmarshal)
	}
	return &request, nil
}

func inferenceAPIKey(storage *zaiStorage) (string, error) {
	if apiKey := strings.TrimSpace(storage.APIKey); apiKey != "" {
		return apiKey, nil
	}
	return "", pluginabi.NewError("invalid_request", "z.ai credential has no API key; sign in again to provision one", http.StatusBadRequest)
}

func sanitizeExecutorError(message, apiKey string) string {
	return sanitizeDetail(redactSecrets(message, []string{apiKey}))
}

func executorUpstreamURL(request *executorRequest) (string, error) {
	sourceFormat := normalizeExecutorFormat(request.SourceFormat)
	outputFormat := normalizeExecutorFormat(request.Format)
	if sourceFormat != "" && outputFormat != "" && sourceFormat != outputFormat {
		return "", pluginabi.NewError("invalid_request", fmt.Sprintf("unsupported mixed z.ai formats %q to %q", request.SourceFormat, request.Format), http.StatusBadRequest)
	}
	format := sourceFormat
	if format == "" {
		format = outputFormat
	}
	switch format {
	case "", "openai":
		return codingChatCompletionsURL, nil
	case "openai-response":
		return codingResponsesURL, nil
	default:
		return "", pluginabi.NewError("invalid_request", fmt.Sprintf("unsupported z.ai executor format %q", format), http.StatusBadRequest)
	}
}

func normalizeExecutorFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "openai", "chat-completions", "openai-chat-completions":
		return "openai"
	case "openai-response", "openai-responses", "responses":
		return "openai-response"
	default:
		return strings.ToLower(strings.TrimSpace(format))
	}
}

// handleExecute forwards the host-translated payload with the provisioned API
// key. Payload rules were already applied by the host; this is the final barrier.
func handleExecute(payload []byte) (any, error) {
	request, errDecode := decodeExecutorRequest(payload)
	if errDecode != nil {
		return nil, errDecode
	}
	if request.Stream {
		return nil, pluginabi.NewError("invalid_request", "streaming requests must use executor.execute_stream", 400)
	}
	storage, errParse := parseZaiStorage(request.StorageJSON)
	if errParse != nil {
		return nil, errParse
	}
	apiKey, errKey := inferenceAPIKey(storage)
	if errKey != nil {
		return nil, errKey
	}
	upstreamURL, errURL := executorUpstreamURL(request)
	if errURL != nil {
		return nil, errURL
	}
	response, errCall := hostDo(request.HostCallbackID, "POST", upstreamURL, codingRequestHeaders(apiKey, storage.ZcodeJWT), request.Payload)
	if errCall != nil {
		return nil, fmt.Errorf("z.ai inference request failed: %s", sanitizeExecutorError(errCall.Error(), apiKey))
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, upstreamStatusError(response.StatusCode, response.Body, apiKey)
	}
	if envelopeAuthRejected(response.Body) {
		return nil, pluginabi.NewError("upstream_error", upstreamErrorDetail(response.Body, apiKey), http.StatusUnauthorized)
	}
	if businessEnvelopeFailed(response.Body) {
		return nil, pluginabi.NewError("upstream_error", upstreamErrorDetail(response.Body, apiKey), http.StatusBadGateway)
	}
	return executorResponse{
		Payload: response.Body,
		Headers: contentTypeOnly(response.Headers),
	}, nil
}

// handleExecuteStream opens the upstream SSE stream through the host and
// answers asynchronously: headers only, chunks pushed via host.stream.emit.
func handleExecuteStream(payload []byte) (any, error) {
	request, errDecode := decodeExecutorRequest(payload)
	if errDecode != nil {
		return nil, errDecode
	}
	if request.StreamID == "" {
		return nil, pluginabi.NewError("invalid_request", "execute_stream requires stream_id", 400)
	}
	storage, errParse := parseZaiStorage(request.StorageJSON)
	if errParse != nil {
		return nil, errParse
	}
	apiKey, errKey := inferenceAPIKey(storage)
	if errKey != nil {
		return nil, errKey
	}
	if _, errURL := executorUpstreamURL(request); errURL != nil {
		return nil, errURL
	}
	stream, errOpen := openUpstreamStream(request, apiKey, storage.ZcodeJWT)
	if errOpen != nil {
		return nil, fmt.Errorf("z.ai inference stream failed: %s", sanitizeExecutorError(errOpen.Error(), apiKey))
	}
	if stream.StatusCode < 200 || stream.StatusCode > 201 {
		closeHTTPStream(stream.StreamID)
		return nil, upstreamStatusError(stream.StatusCode, nil, apiKey)
	}
	streamPumps.Add(1)
	go pumpUpstreamStream(request, apiKey, stream)
	return executorStreamResponse{Headers: map[string][]string{"Content-Type": {"text/event-stream"}}}, nil
}

func openUpstreamStream(request *executorRequest, candidate, zcodeJWT string) (*hostHTTPStreamStart, error) {
	upstreamURL, errURL := executorUpstreamURL(request)
	if errURL != nil {
		return nil, errURL
	}
	result, errCall := callHost(pluginabi.MethodHostHTTPDoStream, hostHTTPRequest{
		HostCallbackID: request.HostCallbackID,
		Request: hostHTTPRequestSpec{
			Method:  "POST",
			URL:     upstreamURL,
			Headers: codingRequestHeaders(candidate, zcodeJWT),
			Body:    request.Payload,
		},
	})
	if errCall != nil {
		return nil, errCall
	}
	var start hostHTTPStreamStart
	if errUnmarshal := json.Unmarshal(result, &start); errUnmarshal != nil {
		return nil, fmt.Errorf("zai: decode host stream start: %w", errUnmarshal)
	}
	return &start, nil
}

// pumpUpstreamStream relays raw upstream SSE bytes from the host HTTP stream
// (httpStreamID) to the plugin RPC stream (rpcStreamID). The two ids belong to
// different bridges and must not be swapped. Chunks are forwarded unmodified.
func pumpUpstreamStream(request *executorRequest, apiKey string, stream *hostHTTPStreamStart) {
	defer streamPumps.Done()
	defer closeHTTPStream(stream.StreamID)
	rpcStreamID := request.StreamID
	pending := []byte{}
	for {
		result, errCall := callHost(pluginabi.MethodHostHTTPStreamRead, hostStreamID{StreamID: stream.StreamID})
		if errCall != nil {
			failRPCStream(rpcStreamID, errCall.Error(), apiKey)
			return
		}
		var read hostHTTPStreamRead
		if errUnmarshal := json.Unmarshal(result, &read); errUnmarshal != nil {
			failRPCStream(rpcStreamID, errUnmarshal.Error(), apiKey)
			return
		}
		if read.Error != "" {
			failRPCStream(rpcStreamID, read.Error, apiKey)
			return
		}
		payload := read.Payload
		if pending != nil {
			pending = append(pending, read.Payload...)
			trimmed := bytes.TrimSpace(pending)
			if len(trimmed) == 0 && !read.Done {
				continue
			}
			if len(trimmed) > 0 && trimmed[0] == '{' && !json.Valid(pending) && !read.Done {
				continue
			}
			payload = pending
			pending = nil
		}
		if envelopeAuthRejected(payload) {
			failRPCStream(rpcStreamID, upstreamErrorDetail(payload, apiKey), apiKey)
			return
		}
		if businessEnvelopeFailed(payload) {
			failRPCStream(rpcStreamID, upstreamErrorDetail(payload, apiKey), apiKey)
			return
		}
		if len(payload) > 0 {
			if _, errEmit := callHost(pluginabi.MethodHostStreamEmit, rpcStreamEmit{StreamID: rpcStreamID, Payload: payload}); errEmit != nil {
				closeRPCStream(rpcStreamID, sanitizeExecutorError(errEmit.Error(), apiKey))
				return
			}
		}
		if read.Done {
			closeRPCStream(rpcStreamID, "")
			return
		}
	}
}

// failRPCStream reports one stream error to the client, then closes the stream
// with the same message.
func failRPCStream(rpcStreamID, message string, secrets ...string) {
	message = sanitizeDetail(redactSecrets(message, secrets))
	if _, errEmit := callHost(pluginabi.MethodHostStreamEmit, rpcStreamEmit{StreamID: rpcStreamID, Error: message}); errEmit != nil {
		message = sanitizeDetail(redactSecrets(errEmit.Error(), secrets))
	}
	closeRPCStream(rpcStreamID, message)
}

func closeRPCStream(rpcStreamID, message string) {
	_, _ = callHost(pluginabi.MethodHostStreamClose, rpcStreamClose{StreamID: rpcStreamID, Error: message})
}

// closeHTTPStream releases a host HTTP stream best-effort.
func closeHTTPStream(httpStreamID string) {
	_, _ = callHost(pluginabi.MethodHostHTTPStreamClose, hostStreamID{StreamID: httpStreamID})
}

// handleCountTokens rejects token counting without an upstream call: the coding
// endpoint does not expose it, and invented counts are worse than errors.
func handleCountTokens([]byte) (any, error) {
	return nil, pluginabi.NewError("unsupported_operation", "z.ai coding endpoint does not expose token counting", 501)
}

// handleExecutorHTTP forwards an executor-owned HTTP request untouched, but
// only to the verified api.z.ai origin. No credentials are attached.
func handleExecutorHTTP(payload []byte) (any, error) {
	var request executorHTTPRequest
	if errUnmarshal := json.Unmarshal(payload, &request); errUnmarshal != nil {
		return nil, fmt.Errorf("zai: decode executor.http_request: %w", errUnmarshal)
	}
	if !allowedZaiOrigin(request.URL) {
		return nil, pluginabi.NewError("forbidden_origin", "only api.z.ai origins are allowed", 403)
	}
	response, errCall := hostDo(request.HostCallbackID, request.Method, request.URL, request.Headers, request.Body)
	if errCall != nil {
		return nil, errCall
	}
	// hostHTTPResponse carries the pluginapi.ExecutorHTTPResponse wire shape.
	return hostHTTPResponse{StatusCode: response.StatusCode, Headers: response.Headers, Body: response.Body}, nil
}

// allowedZaiOrigin admits only https URLs on the api.z.ai host.
func allowedZaiOrigin(rawURL string) bool {
	parsed, errParse := url.Parse(rawURL)
	return errParse == nil && parsed.Scheme == "https" && parsed.Hostname() == "api.z.ai"
}

func businessEnvelopeFailed(body []byte) bool {
	var envelope struct {
		Code    flexNumber `json:"code"`
		Success *bool      `json:"success"`
	}
	if errUnmarshal := json.Unmarshal(body, &envelope); errUnmarshal != nil {
		return false
	}
	if envelope.Success != nil && !*envelope.Success {
		return true
	}
	code := envelope.Code.String()
	return code != "" && code != "0" && code != "200"
}

// codingRequestHeaders pairs the bearer candidate with the separate ZCode
// token used by the ZCode client. It never substitutes one token for the other.
func codingRequestHeaders(bearer, zcodeJWT string) map[string][]string {
	headers := map[string][]string{
		"Content-Type":  {"application/json"},
		"Accept":        {"application/json"},
		"Authorization": {"Bearer " + bearer},
	}
	if token := strings.TrimSpace(zcodeJWT); token != "" {
		headers["X-Zcode-Token"] = []string{token}
	}
	return headers
}

// upstreamStatusError maps a non-2xx upstream response into a client-visible
// error carrying the upstream status. Only the sanitized body detail is kept.
func upstreamStatusError(status int, body []byte, secrets ...string) error {
	message := upstreamErrorDetail(body, secrets...)
	if message == "" {
		message = http.StatusText(status)
	}
	return pluginabi.NewError("upstream_error", message, status)
}

// contentTypeOnly keeps the upstream content type for the executor response;
// other response headers belong to the host transport, not the client.
func contentTypeOnly(headers map[string][]string) map[string][]string {
	if values := headerValue(headers, "Content-Type"); len(values) > 0 {
		return map[string][]string{"Content-Type": values}
	}
	return nil
}

func headerValue(headers map[string][]string, name string) []string {
	for key, values := range headers {
		if strings.EqualFold(key, name) {
			return values
		}
	}
	return nil
}
