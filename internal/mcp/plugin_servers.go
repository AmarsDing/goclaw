package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	shellwords "github.com/mattn/go-shellwords"

	"github.com/nextlevelbuilder/goclaw/internal/plugins"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// PluginMCPServerName returns the stable logical server key for a plugin-declared MCP server.
// It is used as the Pool Acquire "name" so connections are tenant-scoped and deduplicated.
func PluginMCPServerName(pluginName, declName string) string {
	return "plugin:" + pluginName + ":" + declName
}

// ConnectPluginMCPServers registers MCP servers from a plugin manifest using the same paths as
// DB-backed servers: shared Pool.Acquire when a pool is configured, otherwise a dedicated connection.
func (m *Manager) ConnectPluginMCPServers(ctx context.Context, tenantID uuid.UUID, pluginName string, decls []plugins.MCPServerDecl) error {
	if m == nil || len(decls) == 0 {
		return nil
	}

	var connected []string
	for _, decl := range decls {
		if decl.Name == "" {
			return fmt.Errorf("plugin %q: MCP server name is required", pluginName)
		}
		serverName := PluginMCPServerName(pluginName, decl.Name)

		m.mu.RLock()
		_, exists := m.servers[serverName]
		m.mu.RUnlock()
		if exists {
			slog.Debug("mcp.plugin_server.skip", "server", serverName, "reason", "already_connected")
			continue
		}

		transport := strings.TrimSpace(strings.ToLower(decl.Transport))
		var cmd string
		var args []string
		var err error
		switch transport {
		case "stdio":
			if strings.TrimSpace(decl.Command) == "" {
				return fmt.Errorf("plugin %q MCP %q: stdio transport requires command", pluginName, decl.Name)
			}
			cmd, args, err = parsePluginCommand(decl.Command)
			if err != nil {
				return fmt.Errorf("plugin %q MCP %q: command: %w", pluginName, decl.Name, err)
			}
		case "sse", "streamable-http":
			if strings.TrimSpace(decl.URL) == "" {
				return fmt.Errorf("plugin %q MCP %q: transport %q requires url", pluginName, decl.Name, transport)
			}
		default:
			return fmt.Errorf("plugin %q MCP %q: unsupported transport %q (use stdio, sse, streamable-http)", pluginName, decl.Name, decl.Transport)
		}

		timeoutSec := 60
		var connectErr error
		if m.pool != nil {
			connectErr = m.connectViaPool(ctx, tenantID, serverName, transport, cmd, args, nil, decl.URL, nil, "", timeoutSec)
		} else {
			connectErr = m.connectServer(ctx, serverName, transport, cmd, args, nil, decl.URL, nil, "", timeoutSec)
		}
		if connectErr != nil {
			return fmt.Errorf("plugin %q MCP %q: %w", pluginName, decl.Name, connectErr)
		}
		connected = append(connected, serverName)
		slog.Info("mcp.plugin_server.connected", "plugin", pluginName, "server", serverName, "transport", transport)
	}

	if len(connected) == 0 {
		return nil
	}

	m.mu.Lock()
	if m.pluginMCPServers == nil {
		m.pluginMCPServers = make(map[string][]string)
	}
	m.pluginMCPServers[pluginName] = append(m.pluginMCPServers[pluginName], connected...)
	m.mu.Unlock()

	m.maybeEnterSearchMode()
	return nil
}

// DisconnectPluginMCPServers tears down MCP servers previously registered for this plugin name.
func (m *Manager) DisconnectPluginMCPServers(pluginName string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.pluginMCPServers == nil {
		m.mu.Unlock()
		return
	}
	names := append([]string(nil), m.pluginMCPServers[pluginName]...)
	delete(m.pluginMCPServers, pluginName)
	m.mu.Unlock()

	for _, name := range names {
		m.disconnectServerByName(name)
	}
}

func (m *Manager) disconnectServerByName(name string) {
	m.mu.Lock()
	ss, ok := m.servers[name]
	if !ok {
		m.mu.Unlock()
		return
	}
	isPool := false
	if m.poolServers != nil {
		if _, ok := m.poolServers[name]; ok {
			isPool = true
		}
	}

	if isPool {
		var toolNames []string
		if m.poolToolNames != nil {
			toolNames = append([]string(nil), m.poolToolNames[name]...)
		}
		var pkey string
		if m.poolKeys != nil {
			pkey = m.poolKeys[name]
		}
		delete(m.servers, name)
		delete(m.poolServers, name)
		delete(m.poolToolNames, name)
		delete(m.poolKeys, name)
		m.mu.Unlock()

		for _, tn := range toolNames {
			m.registry.Unregister(tn)
		}
		if m.pool != nil && pkey != "" {
			m.pool.Release(pkey)
		}
		tools.UnregisterToolGroup("mcp:" + name)
		m.updateMCPGroup()
		return
	}

	if ss.cancel != nil {
		ss.cancel()
	}
	if client := ss.clientPtr.Load(); client != nil {
		_ = client.Close()
	}
	toolNames := append([]string(nil), ss.toolNames...)
	delete(m.servers, name)
	m.mu.Unlock()

	for _, tn := range toolNames {
		m.registry.Unregister(tn)
	}
	tools.UnregisterToolGroup("mcp:" + name)
	m.updateMCPGroup()
}

func parsePluginCommand(cmdline string) (command string, args []string, err error) {
	parser := shellwords.NewParser()
	fields, err := parser.Parse(cmdline)
	if err != nil {
		return "", nil, err
	}
	if len(fields) == 0 {
		return "", nil, fmt.Errorf("empty command")
	}
	return fields[0], fields[1:], nil
}
