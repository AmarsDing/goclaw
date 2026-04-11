package config

import (
	"net/url"
	"strings"
)

// ResolveVLLMOpenAIBase normalizes user input to a vLLM OpenAI-compatible API root
// (path ending in /v1, no trailing slash), matching NewOpenAIProvider expectations.
func ResolveVLLMOpenAIBase(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		s = "http://localhost:8000"
	}
	s = strings.TrimRight(s, "/")
	u, err := url.Parse(s)
	if err != nil {
		return DockerLocalhost(s + "/v1")
	}
	path := strings.TrimSuffix(u.Path, "/")
	if path == "" || path == "/" {
		u.Path = "/v1"
	}
	out := strings.TrimRight(u.String(), "/")
	return DockerLocalhost(out)
}
