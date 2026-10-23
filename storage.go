package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Verified and candidate z.ai endpoints. The login/poll contract is a
// third-party report, not an official integration contract; see
// docs/compatibility.md before changing these.
const (
	apiZaiBase   = "https://api.z.ai"
	zcodeBase    = "https://zcode.z.ai"
	chatZaiBase  = "https://chat.z.ai"
	monitorBase  = apiZaiBase + "/api/monitor"
	businessBase = apiZaiBase + "/api"

	codingChatCompletionsURL = apiZaiBase + "/api/coding/paas/v4/chat/completions"
	usageDetailURL           = monitorBase + "/token-usage/usage-detail"
	quotaLimitURL            = monitorBase + "/usage/quota/limit"
	businessLoginURL         = apiZaiBase + "/api/auth/z/login"
	customerInfoURL          = apiZaiBase + "/api/biz/customer/getCustomerInfo"
	cliInitURL               = zcodeBase + "/api/v1/oauth/cli/init"
	cliPollURL               = zcodeBase + "/api/v1/oauth/cli/poll/"
)

// storageVersion tags the persisted credential schema in AuthData.StorageJSON.
const storageVersion = 1

// zaiStorage is the provider-owned credential record persisted by CPA. Tokens
// are kept by purpose; they are never interchangeable. No API key is ever
// created, stored, or required by this plugin.
type zaiStorage struct {
	Version          int    `json:"version"`
	Type             string `json:"type"`
	OAuthAccessToken string `json:"oauth_access_token,omitempty"`
	ZcodeJWT         string `json:"zcode_jwt,omitempty"`
	BizAccessToken   string `json:"biz_access_token,omitempty"`
	Email            string `json:"email,omitempty"`
	UserID           string `json:"user_id,omitempty"`
	ObtainedAt       string `json:"obtained_at,omitempty"`
	ExpiresAt        string `json:"expires_at,omitempty"`
}

// errNoCredentials reports a storage record without usable login tokens.
var errNoCredentials = fmt.Errorf("zai: stored credentials contain no login token")

func parseZaiStorage(raw []byte) (*zaiStorage, error) {
	var storage zaiStorage
	if errUnmarshal := json.Unmarshal(raw, &storage); errUnmarshal != nil {
		return nil, fmt.Errorf("zai: decode stored credentials: %w", errUnmarshal)
	}
	if storage.Type != providerID {
		return nil, fmt.Errorf("zai: stored credentials have type %q", storage.Type)
	}
	if len(storage.bearerCandidates()) == 0 {
		return nil, errNoCredentials
	}
	return &storage, nil
}

// bearerCandidates lists login-derived tokens in preference order: the short
// lived OAuth session token first, then the durable business token.
func (s *zaiStorage) bearerCandidates() []string {
	out := make([]string, 0, 3)
	for _, token := range []string{s.OAuthAccessToken, s.BizAccessToken, s.ZcodeJWT} {
		token = strings.TrimSpace(token)
		if token != "" {
			out = append(out, token)
		}
	}
	return out
}

func (s *zaiStorage) accountLabel() string {
	if s.Email != "" {
		return s.Email
	}
	return s.UserID
}

// authID derives the stable CPA auth identifier for the account.
func (s *zaiStorage) authID() string {
	id := s.accountLabel()
	if id == "" {
		id = "account"
	}
	return providerID + "-" + sanitizeAuthIDSuffix(id)
}

func sanitizeAuthIDSuffix(id string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(id) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
		default:
			builder.WriteRune('-')
		}
	}
	out := builder.String()
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	return strings.Trim(out, "-")
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// encodeQuery builds a query string with stable encoding for upstream calls.
func encodeQuery(params map[string]string) string {
	values := url.Values{}
	for key, value := range params {
		if value != "" {
			values.Set(key, value)
		}
	}
	return values.Encode()
}
