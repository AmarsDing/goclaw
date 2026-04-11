package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestProvidersHandlerRegisterInMemoryAppliesCodexPoolDefaults(t *testing.T) {
	providerReg := providers.NewRegistry(nil)
	handler := NewProvidersHandler(newMockProviderStore(), newMockSecretsStore(), providerReg, "")

	provider := &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		TenantID:     uuid.New(),
		Name:         "openai-codex",
		ProviderType: store.ProviderChatGPTOAuth,
		APIKey:       "token",
		Enabled:      true,
		Settings: json.RawMessage(`{
			"codex_pool": {
				"strategy": "round_robin",
				"extra_provider_names": ["codex-work"]
			}
		}`),
	}

	handler.registerInMemory(provider)

	runtimeProvider, err := providerReg.GetForTenant(provider.TenantID, provider.Name)
	if err != nil {
		t.Fatalf("GetForTenant() error = %v", err)
	}
	codex, ok := runtimeProvider.(*providers.CodexProvider)
	if !ok {
		t.Fatalf("runtime provider = %T, want *providers.CodexProvider", runtimeProvider)
	}
	defaults := codex.RoutingDefaults()
	if defaults == nil {
		t.Fatal("RoutingDefaults() = nil, want defaults")
	}
	if defaults.Strategy != store.ChatGPTOAuthStrategyRoundRobin {
		t.Fatalf("Strategy = %q, want %q", defaults.Strategy, store.ChatGPTOAuthStrategyRoundRobin)
	}
	if len(defaults.ExtraProviderNames) != 1 || defaults.ExtraProviderNames[0] != "codex-work" {
		t.Fatalf("ExtraProviderNames = %#v, want [\"codex-work\"]", defaults.ExtraProviderNames)
	}
}

func setupProvidersAdminToken(t *testing.T) string {
	t.Helper()
	token := "system-admin-key"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(token): {
			ID:     uuid.New(),
			Scopes: []string{"operator.admin"},
		},
	})
	return token
}

func TestProvidersHandlerCreateRejectsIncompatibleEmbeddingDimensions(t *testing.T) {
	token := setupProvidersAdminToken(t)
	providerStore := newMockProviderStore()
	handler := NewProvidersHandler(providerStore, newMockSecretsStore(), nil, "")
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	body := `{
		"name": "voyage",
		"provider_type": "openai_compat",
		"api_base": "https://api.voyageai.com/v1",
		"enabled": true,
		"settings": {
			"embedding": {
				"enabled": true,
				"model": "voyage-4-nano",
				"dimensions": 2048
			}
		}
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/providers", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if !strings.Contains(w.Body.String(), "1536") {
		t.Fatalf("response body = %q, want mention of 1536", w.Body.String())
	}
	if len(providerStore.providers) != 0 {
		t.Fatalf("provider store mutated on invalid create: %#v", providerStore.providers)
	}
}

func TestProvidersHandlerCreateAllows1536EmbeddingDimensions(t *testing.T) {
	token := setupProvidersAdminToken(t)
	providerStore := newMockProviderStore()
	handler := NewProvidersHandler(providerStore, newMockSecretsStore(), nil, "")
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	body := `{
		"name": "gemini-emb",
		"provider_type": "gemini_native",
		"api_base": "https://generativelanguage.googleapis.com/v1beta/openai",
		"api_key": "token",
		"enabled": true,
		"settings": {
			"embedding": {
				"enabled": true,
				"model": "gemini-embedding-001",
				"dimensions": 1536
			}
		}
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/providers", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status code = %d, want %d, body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if len(providerStore.providers) != 1 {
		t.Fatalf("provider count = %d, want 1", len(providerStore.providers))
	}
}

func TestProvidersHandlerUpdateRejectsIncompatibleEmbeddingDimensions(t *testing.T) {
	token := setupProvidersAdminToken(t)
	providerStore := newMockProviderStore()
	provider := &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		Name:         "voyage",
		ProviderType: store.ProviderOpenAICompat,
		APIBase:      "https://api.voyageai.com/v1",
		Enabled:      true,
		Settings:     json.RawMessage(`{"embedding":{"enabled":true,"model":"voyage-4-nano","dimensions":1536}}`),
	}
	if err := providerStore.CreateProvider(context.Background(), provider); err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}

	handler := NewProvidersHandler(providerStore, newMockSecretsStore(), nil, "")
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	body := `{
		"settings": {
			"embedding": {
				"enabled": true,
				"model": "voyage-4-nano",
				"dimensions": 2048
			}
		}
	}`

	req := httptest.NewRequest(http.MethodPut, "/v1/providers/"+provider.ID.String(), bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusBadRequest)
	}
	current, err := providerStore.GetProvider(context.Background(), provider.ID)
	if err != nil {
		t.Fatalf("GetProvider() error = %v", err)
	}
	es := store.ParseEmbeddingSettings(current.Settings)
	if es == nil || es.Dimensions != 1536 {
		t.Fatalf("embedding dimensions = %+v, want 1536 preserved", es)
	}
}

func TestProvidersHandlerRegisterInMemoryVLLMPrivateBase(t *testing.T) {
	providerReg := providers.NewRegistry(nil)
	handler := NewProvidersHandler(newMockProviderStore(), newMockSecretsStore(), providerReg, "")

	tenantID := uuid.New()
	p := &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		TenantID:     tenantID,
		Name:         "local-vllm",
		ProviderType: store.ProviderVLLM,
		APIBase:      "http://192.168.4.79:8000",
		APIKey:       "",
		Enabled:      true,
	}

	handler.registerInMemory(p)

	runtimeProvider, err := providerReg.GetForTenant(tenantID, "local-vllm")
	if err != nil {
		t.Fatalf("GetForTenant() error = %v", err)
	}
	oai, ok := runtimeProvider.(*providers.OpenAIProvider)
	if !ok {
		t.Fatalf("runtime provider = %T, want *providers.OpenAIProvider", runtimeProvider)
	}
	if oai.ProviderType() != store.ProviderVLLM {
		t.Fatalf("ProviderType() = %q, want %q", oai.ProviderType(), store.ProviderVLLM)
	}
	if oai.APIKey() != "-" {
		t.Fatalf("APIKey() = %q, want placeholder %q", oai.APIKey(), "-")
	}
	wantBase := "http://192.168.4.79:8000/v1"
	if oai.APIBase() != wantBase {
		t.Fatalf("APIBase() = %q, want %q", oai.APIBase(), wantBase)
	}
}

func TestProvidersHandlerCreateVLLMPrivateBaseNoKey(t *testing.T) {
	token := setupProvidersAdminToken(t)
	providerStore := newMockProviderStore()
	providerReg := providers.NewRegistry(nil)
	handler := NewProvidersHandler(providerStore, newMockSecretsStore(), providerReg, "")
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	body := `{
		"name": "vllm-lan",
		"provider_type": "vllm",
		"api_base": "http://10.0.0.5:8000",
		"enabled": true
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/providers", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status code = %d, want %d, body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if len(providerStore.providers) != 1 {
		t.Fatalf("provider count = %d, want 1", len(providerStore.providers))
	}
	var created store.LLMProviderData
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	runtimeProvider, err := providerReg.GetForTenant(created.TenantID, "vllm-lan")
	if err != nil {
		t.Fatalf("GetForTenant() error = %v", err)
	}
	oai, ok := runtimeProvider.(*providers.OpenAIProvider)
	if !ok {
		t.Fatalf("runtime provider = %T, want *providers.OpenAIProvider", runtimeProvider)
	}
	if oai.APIBase() != "http://10.0.0.5:8000/v1" {
		t.Fatalf("APIBase() = %q", oai.APIBase())
	}
}

func TestProvidersHandlerCreateOpenRouterRejectsPrivateBase(t *testing.T) {
	token := setupProvidersAdminToken(t)
	providerStore := newMockProviderStore()
	handler := NewProvidersHandler(providerStore, newMockSecretsStore(), nil, "")
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	body := `{
		"name": "or-private",
		"provider_type": "openrouter",
		"api_base": "https://192.168.1.1/v1",
		"api_key": "sk-test",
		"enabled": true
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/providers", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d, body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if len(providerStore.providers) != 0 {
		t.Fatalf("provider store should be empty on reject, got %d", len(providerStore.providers))
	}
}
