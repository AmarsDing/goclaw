package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func captureEmbeddingRequest(t *testing.T, es *store.EmbeddingSettings) map[string]any {
	t.Helper()

	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2]}]}`))
	}))
	defer server.Close()

	provider := &store.LLMProviderData{
		Name:         "embedding-provider",
		ProviderType: store.ProviderOpenAICompat,
		APIKey:       "test-key",
		APIBase:      server.URL,
		Enabled:      true,
	}

	ep := buildEmbeddingProvider(provider, es, nil, nil)
	if ep == nil {
		t.Fatal("buildEmbeddingProvider() = nil, want provider")
	}
	if _, err := ep.Embed(context.Background(), []string{"hello"}); err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	return requestBody
}

func TestBuildEmbeddingProviderDefaultsTo1536Dimensions(t *testing.T) {
	requestBody := captureEmbeddingRequest(t, nil)
	if got := requestBody["dimensions"]; got != float64(1536) {
		t.Fatalf("dimensions = %v, want 1536", got)
	}
}

func TestBuildEmbeddingProviderIgnoresIncompatibleStoredDimensions(t *testing.T) {
	requestBody := captureEmbeddingRequest(t, &store.EmbeddingSettings{
		Enabled:    true,
		Model:      "voyage-4-nano",
		Dimensions: 2048,
	})
	if got := requestBody["dimensions"]; got != float64(1536) {
		t.Fatalf("dimensions = %v, want fallback 1536", got)
	}
}

func TestBuildEmbeddingProviderVLLMOmitsDimensionsParam(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2]}]}`))
	}))
	defer server.Close()

	provider := &store.LLMProviderData{
		Name:         "vllm-emb",
		ProviderType: store.ProviderVLLM,
		APIKey:       "-",
		APIBase:      server.URL,
		Enabled:      true,
	}
	es := &store.EmbeddingSettings{Enabled: true, Model: "google/embeddinggemma-2b-embeddings", Dimensions: 1536}

	ep := buildEmbeddingProvider(provider, es, nil, nil)
	if ep == nil {
		t.Fatal("buildEmbeddingProvider() = nil, want provider")
	}
	if _, err := ep.Embed(context.Background(), []string{"hello"}); err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if _, ok := requestBody["dimensions"]; ok {
		t.Fatalf("dimensions should be omitted for vLLM, got %#v", requestBody["dimensions"])
	}
}

func TestBuildEmbeddingProviderVLLMEmptyAPIKeyNoRegistry(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2]}]}`))
	}))
	defer server.Close()

	provider := &store.LLMProviderData{
		Name:         "vllm-no-key",
		ProviderType: store.ProviderVLLM,
		APIKey:       "",
		APIBase:      server.URL,
		Enabled:      true,
	}
	es := &store.EmbeddingSettings{Enabled: true, Model: "gemma-2b-embeddings"}

	ep := buildEmbeddingProvider(provider, es, nil, nil)
	if ep == nil {
		t.Fatal("buildEmbeddingProvider() = nil, want provider (vLLM allows empty API key)")
	}
	if _, err := ep.Embed(context.Background(), []string{"hello"}); err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if _, ok := requestBody["dimensions"]; ok {
		t.Fatalf("dimensions should be omitted for vLLM")
	}
}

func TestBuildEmbeddingProviderClamps2048To1536(t *testing.T) {
	emb := make([]float32, 2048)
	for i := range emb {
		emb[i] = float32(i + 1)
	}
	payload, err := json.Marshal(map[string]any{
		"data": []map[string]any{{"embedding": emb}},
	})
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/embeddings") {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	provider := &store.LLMProviderData{
		Name:         "vllm-2048",
		ProviderType: store.ProviderVLLM,
		APIKey:       "-",
		APIBase:      server.URL,
		Enabled:      true,
	}
	es := &store.EmbeddingSettings{Enabled: true, Model: "gemma-2b-embeddings"}

	ep := buildEmbeddingProvider(provider, es, nil, nil)
	if ep == nil {
		t.Fatal("buildEmbeddingProvider() = nil")
	}
	vecs, err := ep.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 1 || len(vecs[0]) != 1536 {
		t.Fatalf("len = %d, want 1536", len(vecs[0]))
	}
	if vecs[0][0] != 1 || vecs[0][1535] != 1536 {
		t.Fatalf("unexpected clamped prefix: first=%v last=%v", vecs[0][0], vecs[0][1535])
	}
}
