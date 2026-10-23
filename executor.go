package main

import (
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

// handleExecute forwards the host-translated payload to the coding endpoint
// verbatim and falls back through the stored bearer candidates on 401/403.
// Payload rules were already applied by the host; this is the final barrier.
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
	candidates := storage.bearerCandidates()
	var response *hostHTTPResponse
	for index, candidate := range candidates {
		attempt, errCall := hostDo(nil, request.HostCallbackID, "POST", codingChatCompletionsURL, codingRequestHeaders(candidate), request.Payload)
		if errCall != nil {
			return nil, errCall
		}
		response = attempt
		if !isAuthReject(attempt.StatusCode) || index == len(candidates)-1 {
			break
		}
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, upstreamStatusError(response.StatusCode, response.Body)
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
	candidates := storage.bearerCandidates()
	var stream *hostHTTPStreamStart
	for index, candidate := range candidates {
		result, errCall := callHost(pluginabi.MethodHostHTTPDoStream, hostHTTPRequest{
			HostCallbackID: request.HostCallbackID,
			Request: hostHTTPRequestSpec{
				Method:  "POST",
				URL:     codingChatCompletionsURL,
				Headers: codingRequestHeaders(candidate),
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
		stream = &start
		if !isAuthReject(start.StatusCode) || index == len(candidates)-1 {
			break
		}
		closeHTTPStream(start.StreamID)
	}
	if stream.StatusCode < 200 || stream.StatusCode > 201 {
		closeHTTPStream(stream.StreamID)
		return nil, upstreamStatusError(stream.StatusCode, nil)
	}
	streamPumps.Add(1)
	go pumpUpstreamStream(request.StreamID, stream.StreamID)
	return executorStreamResponse{Headers: map[string][]string{"Content-Type": {"text/event-stream"}}}, nil
}

// pumpUpstreamStream relays raw upstream SSE bytes from the host HTTP stream
// (httpStreamID) to the plugin RPC stream (rpcStreamID). The two ids belong to
// different bridges and must not be swapped. Chunks are forwarded unmodified.
func pumpUpstreamStream(rpcStreamID, httpStreamID string) {
	defer streamPumps.Done()
	for {
		result, errCall := callHost(pluginabi.MethodHostHTTPStreamRead, hostStreamID{StreamID: httpStreamID})
		if errCall != nil {
			failRPCStream(rpcStreamID, sanitizeDetail(errCall.Error()))
			return
		}
		var read hostHTTPStreamRead
		if errUnmarshal := json.Unmarshal(result, &read); errUnmarshal != nil {
			failRPCStream(rpcStreamID, sanitizeDetail(errUnmarshal.Error()))
			return
		}
		if message := sanitizeDetail(read.Error); message != "" {
			failRPCStream(rpcStreamID, message)
			return
		}
		if len(read.Payload) > 0 {
			if _, errEmit := callHost(pluginabi.MethodHostStreamEmit, rpcStreamEmit{StreamID: rpcStreamID, Payload: read.Payload}); errEmit != nil {
				closeRPCStream(rpcStreamID, sanitizeDetail(errEmit.Error()))
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
func failRPCStream(rpcStreamID, message string) {
	if _, errEmit := callHost(pluginabi.MethodHostStreamEmit, rpcStreamEmit{StreamID: rpcStreamID, Error: message}); errEmit != nil {
		message = sanitizeDetail(errEmit.Error())
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
	response, errCall := hostDo(nil, request.HostCallbackID, request.Method, request.URL, request.Headers, request.Body)
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

// isAuthReject reports statuses that justify trying the next bearer candidate.
func isAuthReject(status int) bool {
	return status == 401 || status == 403
}

// codingRequestHeaders builds the fixed upstream header set for one candidate.
func codingRequestHeaders(bearer string) map[string][]string {
	return map[string][]string{
		"Content-Type":  {"application/json"},
		"Accept":        {"application/json"},
		"Authorization": {"Bearer " + bearer},
	}
}

// upstreamStatusError maps a non-2xx upstream response into a client-visible
// error carrying the upstream status. Only the sanitized body detail is kept.
func upstreamStatusError(status int, body []byte) error {
	message := upstreamErrorDetail(body)
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
