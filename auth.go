package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// Login flow parameters: the upstream expiry governs the flow lifetime; the
// poll interval is respected and capped so host polls stay polite.
const (
	defaultLoginTTL  = 5 * time.Minute
	defaultPollEvery = 3 * time.Second
	maxPollEvery     = 30 * time.Second
)

func init() {
	registerHandler(pluginabi.MethodAuthIdentifier, handleAuthIdentifier)
	registerHandler(pluginabi.MethodAuthParse, handleAuthParse)
	registerHandler(pluginabi.MethodAuthLoginStart, handleAuthLoginStart)
	registerHandler(pluginabi.MethodAuthLoginPoll, handleAuthLoginPoll)
	registerHandler(pluginabi.MethodAuthRefresh, handleAuthRefresh)
}

func handleAuthIdentifier([]byte) (any, error) {
	return map[string]string{"identifier": providerID}, nil
}

// loginFlow is the plugin-side state of one provider-hosted login: the CPA
// state is the public key, the poll token is the upstream secret.
type loginFlow struct {
	pollToken    string
	flowID       string
	expiresAt    time.Time
	pollInterval time.Duration
	lastPoll     time.Time
	inFlight     bool
}

var (
	flowsMu sync.Mutex
	flows   = map[string]*loginFlow{}
)

func expireFlowsLocked() {
	now := time.Now()
	for state, flow := range flows {
		if now.After(flow.expiresAt) {
			delete(flows, state)
		}
	}
}

type authStartRequest struct {
	pluginapi.AuthLoginStartRequest
	HostCallbackID string `json:"host_callback_id"`
}

type authPollRequest struct {
	pluginapi.AuthLoginPollRequest
	HostCallbackID string `json:"host_callback_id"`
}

type authRefreshRequest struct {
	pluginapi.AuthRefreshRequest
	HostCallbackID string `json:"host_callback_id"`
}

// cliInitResponse is the upstream envelope of POST cliInitURL (third-party
// report, see docs/compatibility.md).
type cliInitResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		FlowID          string `json:"flow_id"`
		AuthorizeURL    string `json:"authorize_url"`
		ExpiresAt       any    `json:"expires_at"`
		PollIntervalSec int    `json:"poll_interval_sec"`
	} `json:"data"`
}

type cliPollResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Status         string `json:"status"`
		Token          string `json:"token"`
		ZaiAccessToken string `json:"zai.access_token"`
		Zai            struct {
			AccessToken string `json:"access_token"`
		} `json:"zai"`
		User map[string]any `json:"user"`
	} `json:"data"`
}

type bizLoginResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		AccessToken string `json:"access_token"`
	} `json:"data"`
}

type customerInfoResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Organizations []struct {
			OrganizationID string `json:"organizationId"`
			IsDefault      bool   `json:"isDefault"`
			Projects       []struct {
				ProjectID string `json:"projectId"`
				ID        string `json:"id"`
			} `json:"projects"`
		} `json:"organizations"`
	} `json:"data"`
}

func randomHex(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, errRead := rand.Read(buf); errRead != nil {
		return "", fmt.Errorf("generate random token: %w", errRead)
	}
	return hex.EncodeToString(buf), nil
}

// parseUpstreamExpiry accepts the reported expires_at as RFC3339 text or unix
// seconds; absent or unreadable values fall back to the default TTL.
func parseUpstreamExpiry(value any) (time.Time, bool) {
	switch reported := value.(type) {
	case string:
		if reported == "" {
			return time.Time{}, false
		}
		if parsed, errParse := time.Parse(time.RFC3339, reported); errParse == nil {
			return parsed, true
		}
		if secs, errParse := strconv.ParseInt(reported, 10, 64); errParse == nil {
			return time.Unix(secs, 0), true
		}
	case float64:
		return time.Unix(int64(reported), 0), true
	}
	return time.Time{}, false
}

func handleAuthLoginStart(payload []byte) (any, error) {
	var req authStartRequest
	if errUnmarshal := json.Unmarshal(payload, &req); errUnmarshal != nil {
		return nil, fmt.Errorf("decode auth.login.start request: %w", errUnmarshal)
	}
	pollToken, errToken := randomHex(32)
	if errToken != nil {
		return nil, errToken
	}
	body, _ := json.Marshal(map[string]string{"provider": providerID})
	resp, errDo := hostDo(req.HostCallbackID, http.MethodPost, cliInitURL, map[string][]string{
		"Authorization": {"Bearer " + pollToken},
		"Content-Type":  {"application/json"},
	}, body)
	if errDo != nil {
		return nil, errDo
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, upstreamError(resp.StatusCode, resp.Body, pollToken)
	}
	var init cliInitResponse
	if errUnmarshal := json.Unmarshal(resp.Body, &init); errUnmarshal != nil {
		return nil, fmt.Errorf("decode z.ai cli init response: %w", errUnmarshal)
	}
	if init.Code != 0 {
		return nil, fmt.Errorf("z.ai cli init failed: %s", upstreamErrorDetail(resp.Body, pollToken))
	}
	if init.Data.FlowID == "" || init.Data.AuthorizeURL == "" {
		return nil, fmt.Errorf("z.ai cli init response missing flow_id or authorize_url")
	}
	authorizeURL, errParseURL := url.Parse(init.Data.AuthorizeURL)
	if errParseURL != nil || authorizeURL.Scheme != "https" || authorizeURL.Host == "" {
		return nil, fmt.Errorf("z.ai cli init returned an invalid authorize url")
	}
	state, errState := randomHex(32)
	if errState != nil {
		return nil, errState
	}
	query := authorizeURL.Query()
	query.Set("state", state)
	authorizeURL.RawQuery = query.Encode()
	expiresAt := time.Now().Add(defaultLoginTTL)
	if parsed, ok := parseUpstreamExpiry(init.Data.ExpiresAt); ok && parsed.After(time.Now()) && parsed.Before(time.Now().Add(24*time.Hour)) {
		expiresAt = parsed
	}
	interval := time.Duration(init.Data.PollIntervalSec) * time.Second
	if interval <= 0 {
		interval = defaultPollEvery
	}
	if interval > maxPollEvery {
		interval = maxPollEvery
	}
	flowsMu.Lock()
	expireFlowsLocked()
	flows[state] = &loginFlow{
		pollToken:    pollToken,
		flowID:       init.Data.FlowID,
		expiresAt:    expiresAt,
		pollInterval: interval,
	}
	flowsMu.Unlock()
	// The flows map is the single holder of the poll credential; nothing that
	// reaches the browser may carry the upstream poll token or flow id.
	return pluginapi.AuthLoginStartResponse{
		Provider:  providerID,
		URL:       authorizeURL.String(),
		State:     state,
		ExpiresAt: expiresAt,
	}, nil
}

// handleAuthLoginPoll applies the rate limit and single-flight gates, then
// exchanges at most once per flow. The flow is consumed on success only.
func handleAuthLoginPoll(payload []byte) (any, error) {
	var req authPollRequest
	if errUnmarshal := json.Unmarshal(payload, &req); errUnmarshal != nil {
		return nil, fmt.Errorf("decode auth.login.poll request: %w", errUnmarshal)
	}
	flowsMu.Lock()
	flow, ok := flows[req.State]
	if !ok {
		flowsMu.Unlock()
		return pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusError, Message: "z.ai login flow not found"}, nil
	}
	if time.Now().After(flow.expiresAt) {
		delete(flows, req.State)
		flowsMu.Unlock()
		return pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusError, Message: "z.ai login flow expired"}, nil
	}
	if flow.inFlight || (!flow.lastPoll.IsZero() && time.Since(flow.lastPoll) < flow.pollInterval) {
		flowsMu.Unlock()
		return pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusPending}, nil
	}
	flow.lastPoll = time.Now()
	flow.inFlight = true
	flowsMu.Unlock()

	resp, errExchange, consumed := exchangeLoginPoll(req.HostCallbackID, flow)
	flowsMu.Lock()
	flow.inFlight = false
	if consumed {
		delete(flows, req.State)
	}
	flowsMu.Unlock()
	if errExchange != nil {
		return nil, errExchange
	}
	return resp, nil
}

// exchangeLoginPoll runs one upstream poll exchange. consumed reports a
// completed sign-in so the caller can delete the flow (single consumption).
func exchangeLoginPoll(callbackID string, flow *loginFlow) (resp *pluginapi.AuthLoginPollResponse, err error, consumed bool) {
	pollHTTP, errDo := hostDo(callbackID, http.MethodGet, cliPollURL+flow.flowID,
		map[string][]string{"Authorization": {"Bearer " + flow.pollToken}}, nil)
	if errDo != nil {
		return nil, errDo, false
	}
	if pollHTTP.StatusCode < 200 || pollHTTP.StatusCode > 299 {
		message := upstreamError(pollHTTP.StatusCode, pollHTTP.Body, flow.pollToken)
		return &pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusError, Message: "z.ai poll failed: " + message.Error()}, nil, false
	}
	var poll cliPollResponse
	if errUnmarshal := json.Unmarshal(pollHTTP.Body, &poll); errUnmarshal != nil {
		return nil, fmt.Errorf("decode z.ai poll response: %w", errUnmarshal), false
	}
	if poll.Code != 0 {
		return pollError("z.ai poll failed: %s", pollHTTP.Body, flow.pollToken), nil, false
	}
	if poll.Data.Status != "ready" {
		return &pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusPending}, nil, false
	}
	oauthToken := poll.Data.ZaiAccessToken
	if oauthToken == "" {
		oauthToken = poll.Data.Zai.AccessToken
	}
	if poll.Data.Token == "" || oauthToken == "" {
		return &pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusError, Message: "z.ai poll response is ready but carries no tokens"}, nil, false
	}
	bizToken, respFail, errBiz := exchangeBusinessLogin(callbackID, oauthToken)
	if errBiz != nil {
		return nil, errBiz, false
	}
	if respFail != nil {
		return respFail, nil, false
	}
	orgID, projectID := fetchCustomerOrg(callbackID, bizToken)
	storage := zaiStorage{
		Version:          storageVersion,
		Type:             providerID,
		OAuthAccessToken: oauthToken,
		ZcodeJWT:         poll.Data.Token,
		BizAccessToken:   bizToken,
		Email:            pollUserField(poll.Data.User, "email", "userName", "name"),
		UserID:           pollUserField(poll.Data.User, "id", "user_id"),
		OrganizationID:   orgID,
		ProjectID:        projectID,
		ObtainedAt:       nowRFC3339(),
	}
	raw, _ := json.Marshal(storage)
	id := storage.authID()
	return &pluginapi.AuthLoginPollResponse{
		Status:  pluginapi.AuthLoginStatusSuccess,
		Message: "z.ai sign-in complete",
		Auth: pluginapi.AuthData{
			Provider:    providerID,
			ID:          id,
			FileName:    id + ".json",
			Label:       "Z.ai " + storage.accountLabel(),
			StorageJSON: raw,
			Metadata: map[string]any{
				"type":            providerID,
				"organization_id": orgID,
				"project_id":      projectID,
			},
		},
	}, nil, true
}

func pollError(format string, body []byte, secrets ...string) *pluginapi.AuthLoginPollResponse {
	return &pluginapi.AuthLoginPollResponse{
		Status:  pluginapi.AuthLoginStatusError,
		Message: fmt.Sprintf(format, upstreamErrorDetail(body, secrets...)),
	}
}

// exchangeBusinessLogin swaps the z.ai OAuth token for the durable business
// token. The upstream envelope reports code 200 (0 accepted defensively).
func exchangeBusinessLogin(callbackID, oauthToken string) (bizToken string, respFail *pluginapi.AuthLoginPollResponse, err error) {
	body, _ := json.Marshal(map[string]string{"token": oauthToken})
	bizHTTP, errDo := hostDo(callbackID, http.MethodPost, businessLoginURL,
		map[string][]string{"Content-Type": {"application/json"}}, body)
	if errDo != nil {
		return "", nil, errDo
	}
	if bizHTTP.StatusCode < 200 || bizHTTP.StatusCode > 299 {
		return "", pollError("z.ai sign-in failed: %s", bizHTTP.Body, oauthToken), nil
	}
	var biz bizLoginResponse
	if errUnmarshal := json.Unmarshal(bizHTTP.Body, &biz); errUnmarshal != nil {
		return "", nil, fmt.Errorf("decode z.ai business login response: %w", errUnmarshal)
	}
	if biz.Code != 0 && biz.Code != 200 {
		return "", pollError("z.ai sign-in failed: %s", bizHTTP.Body, oauthToken), nil
	}
	if biz.Data.AccessToken == "" {
		return "", &pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusError, Message: "z.ai sign-in response carries no business token"}, nil
	}
	return biz.Data.AccessToken, nil, nil
}

// fetchCustomerOrg enriches the record with the default organization and its
// first project. It is optional context: any failure keeps empty values.
func fetchCustomerOrg(callbackID, bizToken string) (orgID, projectID string) {
	infoHTTP, errDo := hostDo(callbackID, http.MethodGet, customerInfoURL,
		map[string][]string{"Authorization": {"Bearer " + bizToken}}, nil)
	if errDo != nil || infoHTTP.StatusCode < 200 || infoHTTP.StatusCode > 299 {
		return "", ""
	}
	var info customerInfoResponse
	if errUnmarshal := json.Unmarshal(infoHTTP.Body, &info); errUnmarshal != nil {
		return "", ""
	}
	if info.Code != 0 && info.Code != 200 {
		return "", ""
	}
	for _, org := range info.Data.Organizations {
		if !org.IsDefault {
			continue
		}
		return org.OrganizationID, firstProjectID(org.Projects)
	}
	if len(info.Data.Organizations) > 0 {
		org := info.Data.Organizations[0]
		return org.OrganizationID, firstProjectID(org.Projects)
	}
	return "", ""
}

func firstProjectID(projects []struct {
	ProjectID string `json:"projectId"`
	ID        string `json:"id"`
}) string {
	if len(projects) == 0 {
		return ""
	}
	if projects[0].ProjectID != "" {
		return projects[0].ProjectID
	}
	return projects[0].ID
}

func pollUserField(user map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := user[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

// handleAuthParse accepts only records that decode into the zai storage schema.
func handleAuthParse(payload []byte) (any, error) {
	var req pluginapi.AuthParseRequest
	if errUnmarshal := json.Unmarshal(payload, &req); errUnmarshal != nil {
		return nil, fmt.Errorf("decode auth.parse request: %w", errUnmarshal)
	}
	storage, errParse := parseZaiStorage(req.RawJSON)
	if errParse != nil {
		return pluginapi.AuthParseResponse{Handled: false}, nil
	}
	return pluginapi.AuthParseResponse{Handled: true, Auth: authDataFromStorage(storage)}, nil
}

func authDataFromStorage(storage *zaiStorage) pluginapi.AuthData {
	raw, _ := json.Marshal(storage)
	id := storage.authID()
	return pluginapi.AuthData{
		Provider:    providerID,
		ID:          id,
		FileName:    id + ".json",
		Label:       "Z.ai " + storage.accountLabel(),
		StorageJSON: raw,
		Metadata: map[string]any{
			"type":            providerID,
			"organization_id": storage.OrganizationID,
			"project_id":      storage.ProjectID,
		},
	}
}

// handleAuthRefresh deliberately makes zero upstream calls: z.ai offers no
// verified refresh mechanism. Renewal requires a new browser sign-in.
func handleAuthRefresh(payload []byte) (any, error) {
	var req authRefreshRequest
	if errUnmarshal := json.Unmarshal(payload, &req); errUnmarshal != nil {
		return nil, fmt.Errorf("decode auth.refresh request: %w", errUnmarshal)
	}
	if _, errParse := parseZaiStorage(req.StorageJSON); errParse != nil {
		return nil, errParse
	}
	return nil, pluginabi.NewError("unsupported_operation", "z.ai login renewal requires a new browser sign-in", 501)
}
