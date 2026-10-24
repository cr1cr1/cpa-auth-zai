package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

type apiKeyItem struct {
	Name      string `json:"name"`
	APIKey    string `json:"apiKey"`
	ID        string `json:"id"`
	SecretKey string `json:"secretKey"`
	Secret    string `json:"secret"`
}

type apiKeyListResponse struct {
	Code int          `json:"code"`
	Msg  string       `json:"msg"`
	Data []apiKeyItem `json:"data"`
}

type apiKeyResponse struct {
	Code int        `json:"code"`
	Msg  string     `json:"msg"`
	Data apiKeyItem `json:"data"`
}

var apiKeyProvisionMu sync.Mutex

// provisionAPIKey reuses the exact configured project key or creates it once.
func provisionAPIKey(callbackID, bizToken, orgID, projectID string) (string, error) {
	if strings.TrimSpace(bizToken) == "" || strings.TrimSpace(orgID) == "" || strings.TrimSpace(projectID) == "" {
		return "", fmt.Errorf("z.ai API key setup requires business token, organization, and project")
	}
	// Login is infrequent; one lock prevents duplicate check-then-create races.
	apiKeyProvisionMu.Lock()
	defer apiKeyProvisionMu.Unlock()
	keysURL := fmt.Sprintf("%s/api/biz/v1/organization/%s/projects/%s/api_keys", apiZaiBase, url.PathEscape(orgID), url.PathEscape(projectID))
	listHTTP, errDo := hostDo(callbackID, http.MethodGet, keysURL, apiKeyHeaders(bizToken, false), nil)
	if errDo != nil {
		return "", fmt.Errorf("z.ai API key list request failed: %s", upstreamErrorDetail([]byte(errDo.Error()), bizToken))
	}
	var list apiKeyListResponse
	errUnmarshal := json.Unmarshal(listHTTP.Body, &list)
	listSecrets := []string{bizToken}
	for _, item := range list.Data {
		listSecrets = append(listSecrets, apiKeyItemSecrets(item)...)
	}
	if listHTTP.StatusCode < 200 || listHTTP.StatusCode > 299 {
		return "", upstreamError(listHTTP.StatusCode, listHTTP.Body, listSecrets...)
	}
	if errUnmarshal != nil {
		return "", fmt.Errorf("decode z.ai API key list: %w", errUnmarshal)
	}
	if list.Code != 0 && list.Code != 200 {
		return "", fmt.Errorf("z.ai API key list failed: %s", upstreamErrorDetail(listHTTP.Body, listSecrets...))
	}
	name := preferredAPIKeyName()
	var selected *apiKeyItem
	for index := range list.Data {
		if list.Data[index].Name == name {
			selected = &list.Data[index]
			break
		}
	}
	if selected == nil {
		body, _ := json.Marshal(map[string]string{"name": name})
		createHTTP, errCreate := hostDo(callbackID, http.MethodPost, keysURL, apiKeyHeaders(bizToken, true), body)
		if errCreate != nil {
			return "", fmt.Errorf("z.ai API key creation request failed: %s", upstreamErrorDetail([]byte(errCreate.Error()), bizToken))
		}
		var created apiKeyResponse
		errUnmarshal = json.Unmarshal(createHTTP.Body, &created)
		createSecrets := append([]string{bizToken}, apiKeyItemSecrets(created.Data)...)
		if createHTTP.StatusCode < 200 || createHTTP.StatusCode > 299 {
			return "", upstreamError(createHTTP.StatusCode, createHTTP.Body, createSecrets...)
		}
		if errUnmarshal != nil {
			return "", fmt.Errorf("decode z.ai API key creation: %w", errUnmarshal)
		}
		if created.Code != 0 && created.Code != 200 {
			return "", fmt.Errorf("z.ai API key creation failed: %s", upstreamErrorDetail(createHTTP.Body, createSecrets...))
		}
		selected = &created.Data
	}
	keyID := strings.TrimSpace(selected.APIKey)
	if keyID == "" {
		keyID = strings.TrimSpace(selected.ID)
	}
	secret := strings.TrimSpace(selected.SecretKey)
	if secret == "" {
		secret = strings.TrimSpace(selected.Secret)
	}
	if keyID == "" {
		return "", fmt.Errorf("z.ai API key response carries no key ID")
	}
	if secret == "" || strings.Contains(secret, "*") {
		copyHTTP, errCopy := hostDo(callbackID, http.MethodGet, keysURL+"/copy/"+url.PathEscape(keyID), apiKeyHeaders(bizToken, false), nil)
		if errCopy != nil {
			return "", fmt.Errorf("z.ai API key copy request failed: %s", upstreamErrorDetail([]byte(errCopy.Error()), bizToken, keyID, secret))
		}
		var copied apiKeyResponse
		errUnmarshal = json.Unmarshal(copyHTTP.Body, &copied)
		copySecrets := append([]string{bizToken, keyID, secret}, apiKeyItemSecrets(copied.Data)...)
		if copyHTTP.StatusCode < 200 || copyHTTP.StatusCode > 299 {
			return "", upstreamError(copyHTTP.StatusCode, copyHTTP.Body, copySecrets...)
		}
		if errUnmarshal != nil {
			return "", fmt.Errorf("decode z.ai API key copy: %w", errUnmarshal)
		}
		secret = strings.TrimSpace(copied.Data.SecretKey)
		if secret == "" {
			secret = strings.TrimSpace(copied.Data.Secret)
		}
		if copied.Code != 0 && copied.Code != 200 {
			return "", fmt.Errorf("z.ai API key copy failed: %s", upstreamErrorDetail(copyHTTP.Body, copySecrets...))
		}
	}
	if secret == "" || strings.Contains(secret, "*") {
		return "", fmt.Errorf("z.ai API key response carries no readable secret")
	}
	return keyID + "." + secret, nil
}

func apiKeyItemSecrets(item apiKeyItem) []string {
	return []string{item.APIKey, item.ID, item.SecretKey, item.Secret}
}

func apiKeyHeaders(bizToken string, jsonBody bool) map[string][]string {
	headers := map[string][]string{"Authorization": {"Bearer " + bizToken}}
	if jsonBody {
		headers["Content-Type"] = []string{"application/json"}
	}
	return headers
}
