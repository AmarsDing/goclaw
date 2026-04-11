package config

import "testing"

func TestResolveVLLMOpenAIBase(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"", "http://localhost:8000/v1"},
		{"http://192.168.1.5:8000", "http://192.168.1.5:8000/v1"},
		{"http://192.168.1.5:8000/v1", "http://192.168.1.5:8000/v1"},
		{"https://host:9000/v1/", "https://host:9000/v1"},
	}
	for _, tc := range cases {
		if got := ResolveVLLMOpenAIBase(tc.in); got != tc.want {
			t.Errorf("ResolveVLLMOpenAIBase(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
