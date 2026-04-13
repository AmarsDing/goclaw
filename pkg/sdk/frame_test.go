package sdk

import (
	"encoding/json"
	"testing"
)

func TestParseGatewayEventFrame(t *testing.T) {
	raw := json.RawMessage(`{"type":"event","event":"permission.mode_change","payload":{"agent_id":"a","old_mode":"safe","new_mode":"auto"},"seq":1}`)
	f, err := ParseGatewayEventFrame(raw)
	if err != nil {
		t.Fatal(err)
	}
	if f.Event != string(EventPermissionModeChange) {
		t.Fatalf("event = %q", f.Event)
	}
	ev := ToSDKEvent("", "ag", "sk", 1, f)
	if ev.Type != EventPermissionModeChange {
		t.Fatalf("sdk type = %q", ev.Type)
	}
}
