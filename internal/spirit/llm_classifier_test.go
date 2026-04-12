package spirit

import "testing"

func TestExtractJSONObject(t *testing.T) {
	t.Parallel()
	raw := "Here is the result:\n```json\n{\"type\":\"code\",\"confidence\":0.9}\n```"
	out := extractJSONObject(raw)
	if out != `{"type":"code","confidence":0.9}` {
		t.Fatalf("got %q", out)
	}
}

func TestExtractJSONObjectPlain(t *testing.T) {
	t.Parallel()
	raw := `prefix {"type":"research","confidence":0.5} suffix`
	out := extractJSONObject(raw)
	if out != `{"type":"research","confidence":0.5}` {
		t.Fatalf("got %q", out)
	}
}
