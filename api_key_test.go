package main

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProvisionAPIKeyCopiesMaskedSecret(t *testing.T) {
	t.Cleanup(func() { _ = parsePluginConfig(nil) })
	keysURL := "https://api.z.ai/api/biz/v1/organization/org-1/projects/proj-1/api_keys"
	fake := installAuthFakeHost(t, func(call authHostCall) []byte {
		switch call.URL {
		case keysURL:
			if call.Method != http.MethodGet {
				t.Errorf("existing key must not be recreated: %s", call.Method)
			}
			return authHostEnvelope(200, `{"code":200,"data":[{"name":"cpa-auth-zai","apiKey":"existing-id","secretKey":"****"}]}`)
		case keysURL + "/copy/existing-id":
			if call.Method != http.MethodGet || strings.Join(call.Headers["Authorization"], "") != "Bearer biz-jwt" {
				t.Errorf("copy request = %s %v", call.Method, call.Headers)
			}
			return authHostEnvelope(200, `{"code":200,"data":{"secretKey":"copied-secret"}}`)
		}
		t.Errorf("unexpected key request = %s %s", call.Method, call.URL)
		return authHostEnvelope(404, `{"code":404,"msg":"not found"}`)
	})
	key, errProvision := provisionAPIKey("cb-1", "biz-jwt", "org-1", "proj-1")
	if errProvision != nil {
		t.Fatalf("provision API key: %v", errProvision)
	}
	if key != "existing-id.copied-secret" {
		t.Fatalf("API key = %q", key)
	}
	if calls := len(fake.recorded()); calls != 2 {
		t.Fatalf("key-management calls = %d, want list then copy", calls)
	}
}

func TestProvisionAPIKeyRequiresProjectContext(t *testing.T) {
	fake := installAuthFakeHost(t, func(call authHostCall) []byte {
		t.Errorf("unexpected key request = %s %s", call.Method, call.URL)
		return authHostEnvelope(500, `{}`)
	})
	if _, errProvision := provisionAPIKey("cb-1", "biz-jwt", "org-1", ""); errProvision == nil {
		t.Fatalf("missing project must fail")
	}
	if calls := len(fake.recorded()); calls != 0 {
		t.Fatalf("key-management calls = %d, want 0", calls)
	}
}

func TestProvisionAPIKeyDoesNotCreateAfterCopyFailure(t *testing.T) {
	keysURL := "https://api.z.ai/api/biz/v1/organization/org-1/projects/proj-1/api_keys"
	fake := installAuthFakeHost(t, func(call authHostCall) []byte {
		switch call.URL {
		case keysURL:
			return authHostEnvelope(200, `{"code":200,"data":[{"name":"cpa-auth-zai","apiKey":"existing-id","secretKey":"****"}]}`)
		case keysURL + "/copy/existing-id":
			return authHostEnvelope(500, `{"msg":"biz-jwt copy failed"}`)
		}
		t.Errorf("unexpected key request = %s %s", call.Method, call.URL)
		return authHostEnvelope(404, `{}`)
	})
	_, errProvision := provisionAPIKey("cb-1", "biz-jwt", "org-1", "proj-1")
	if errProvision == nil || strings.Contains(errProvision.Error(), "biz-jwt") || !strings.Contains(errProvision.Error(), "[redacted]") {
		t.Fatalf("copy error = %v, want redacted failure", errProvision)
	}
	calls := fake.recorded()
	if len(calls) != 2 || calls[0].Method != http.MethodGet || calls[1].Method != http.MethodGet {
		t.Fatalf("copy failure must not create a key: %+v", calls)
	}
}

func TestProvisionAPIKeyRedactsHostError(t *testing.T) {
	previous := hostCaller
	hostCaller = func(method string, payload []byte) ([]byte, error) {
		return nil, fmt.Errorf("request with Bearer biz-jwt failed")
	}
	t.Cleanup(func() { hostCaller = previous })
	_, errProvision := provisionAPIKey("cb-1", "biz-jwt", "org-1", "proj-1")
	if errProvision == nil || strings.Contains(errProvision.Error(), "biz-jwt") || !strings.Contains(errProvision.Error(), "[redacted]") {
		t.Fatalf("host error = %v, want redacted failure", errProvision)
	}
}

func TestProvisionAPIKeyRedactsListBusinessFailureSecret(t *testing.T) {
	installAuthFakeHost(t, func(call authHostCall) []byte {
		return authHostEnvelope(200, `{"code":500,"msg":"existing-id.existing-secret failed","data":[{"apiKey":"existing-id","secretKey":"existing-secret"}]}`)
	})
	_, errProvision := provisionAPIKey("cb-1", "biz-jwt", "org-1", "proj-1")
	if errProvision == nil || strings.Contains(errProvision.Error(), "existing-id") || strings.Contains(errProvision.Error(), "existing-secret") || !strings.Contains(errProvision.Error(), "[redacted]") {
		t.Fatalf("list error = %v, want redacted key material", errProvision)
	}
}

func TestProvisionAPIKeyRedactsCreateFailureSecret(t *testing.T) {
	installAuthFakeHost(t, func(call authHostCall) []byte {
		if call.Method == http.MethodGet {
			return authHostEnvelope(200, `{"code":200,"data":[]}`)
		}
		return authHostEnvelope(400, `{"code":400,"msg":"new-id.new-secret failed","data":{"apiKey":"new-id","secretKey":"new-secret"}}`)
	})
	_, errProvision := provisionAPIKey("cb-1", "biz-jwt", "org-1", "proj-1")
	if errProvision == nil || strings.Contains(errProvision.Error(), "new-id") || strings.Contains(errProvision.Error(), "new-secret") || !strings.Contains(errProvision.Error(), "[redacted]") {
		t.Fatalf("create error = %v, want redacted key material", errProvision)
	}
}

func TestProvisionAPIKeyConcurrentCreatesOnce(t *testing.T) {
	var mu sync.Mutex
	created := false
	createCalls := 0
	listCalls := 0
	installAuthFakeHost(t, func(call authHostCall) []byte {
		if call.Method == http.MethodPost {
			mu.Lock()
			createCalls++
			created = true
			mu.Unlock()
			return authHostEnvelope(200, `{"code":200,"data":{"name":"cpa-auth-zai","apiKey":"created-id","secretKey":"created-secret"}}`)
		}
		mu.Lock()
		listCalls++
		firstList := listCalls == 1
		alreadyCreated := created
		mu.Unlock()
		if firstList {
			time.Sleep(50 * time.Millisecond)
		}
		if alreadyCreated {
			return authHostEnvelope(200, `{"code":200,"data":[{"name":"cpa-auth-zai","apiKey":"created-id","secretKey":"created-secret"}]}`)
		}
		return authHostEnvelope(200, `{"code":200,"data":[]}`)
	})
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			key, errProvision := provisionAPIKey("cb-1", "biz-jwt", "org-1", "proj-1")
			if errProvision == nil && key != "created-id.created-secret" {
				errProvision = fmt.Errorf("unexpected key %q", key)
			}
			results <- errProvision
		}()
	}
	close(start)
	for range 2 {
		if errProvision := <-results; errProvision != nil {
			t.Fatalf("concurrent provisioning: %v", errProvision)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if createCalls != 1 {
		t.Fatalf("create calls = %d, want 1", createCalls)
	}
}

func TestProvisionAPIKeyCreatesConfiguredName(t *testing.T) {
	t.Cleanup(func() { _ = parsePluginConfig(nil) })
	if errConfigure := parsePluginConfig([]byte("api-key-name: team-cpa\n")); errConfigure != nil {
		t.Fatalf("configure: %v", errConfigure)
	}
	keysURL := "https://api.z.ai/api/biz/v1/organization/org-1/projects/proj-1/api_keys"
	fake := installAuthFakeHost(t, func(call authHostCall) []byte {
		switch {
		case call.URL == keysURL && call.Method == http.MethodGet:
			return authHostEnvelope(200, `{"code":200,"data":[]}`)
		case call.URL == keysURL && call.Method == http.MethodPost:
			if string(call.Body) != `{"name":"team-cpa"}` {
				t.Errorf("create body = %s", call.Body)
			}
			if strings.Join(call.Headers["Authorization"], "") != "Bearer biz-jwt" || strings.Join(call.Headers["Content-Type"], "") != "application/json" {
				t.Errorf("create headers = %v", call.Headers)
			}
			return authHostEnvelope(200, `{"code":200,"data":{"name":"team-cpa","apiKey":"created-id","secretKey":"created-secret"}}`)
		}
		t.Errorf("unexpected key request = %s %s", call.Method, call.URL)
		return authHostEnvelope(404, `{"code":404,"msg":"not found"}`)
	})
	key, errProvision := provisionAPIKey("cb-1", "biz-jwt", "org-1", "proj-1")
	if errProvision != nil {
		t.Fatalf("provision API key: %v", errProvision)
	}
	if key != "created-id.created-secret" {
		t.Fatalf("API key = %q", key)
	}
	if calls := len(fake.recorded()); calls != 2 {
		t.Fatalf("key-management calls = %d, want list then create", calls)
	}
}
