package mcp

import "testing"

func TestPluginMCPServerName(t *testing.T) {
	got := PluginMCPServerName("myplugin", "srv1")
	want := "plugin:myplugin:srv1"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
