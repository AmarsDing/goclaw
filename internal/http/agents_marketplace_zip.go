package http

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

const maxAgentMarketplaceZipSize = 32 << 20

func addToZip(zw *zip.Writer, name string, data []byte) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// buildAgentMarketplaceZip packs agent config, context files, and a small manifest into a zip
// suitable for POST /v1/marketplace/packages/upload (same layout as export, zip instead of tar.gz).
func (h *AgentsHandler) buildAgentMarketplaceZip(ctx context.Context, ag *store.AgentData) ([]byte, error) {
	var buf bytes.Buffer
	lw := &limitedWriter{w: &buf, limit: maxAgentMarketplaceZipSize}
	zw := zip.NewWriter(lw)

	agentJSON, err := marshalAgentConfig(ag)
	if err != nil {
		_ = zw.Close()
		return nil, fmt.Errorf("marshal agent config: %w", err)
	}
	if err := addToZip(zw, "agent.json", agentJSON); err != nil {
		_ = zw.Close()
		return nil, err
	}

	files, err := pg.ExportAgentContextFiles(ctx, h.db, ag.ID)
	if err != nil {
		_ = zw.Close()
		return nil, fmt.Errorf("export context files: %w", err)
	}
	for _, f := range files {
		path := "context_files/" + sanitizeName(f.FileName)
		if err := addToZip(zw, path, []byte(f.Content)); err != nil {
			_ = zw.Close()
			return nil, err
		}
	}

	manifest := map[string]any{
		"format":      "goclaw-agent-marketplace",
		"version":     1,
		"exported_at": time.Now().UTC().Format(time.RFC3339),
		"agent_id":    ag.ID.String(),
		"agent_key":   ag.AgentKey,
		"sections": map[string]any{
			"config":        map[string]int{"count": 1},
			"context_files": map[string]int{"count": len(files)},
		},
	}
	mj, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		_ = zw.Close()
		return nil, err
	}
	if err := addToZip(zw, "manifest.json", mj); err != nil {
		_ = zw.Close()
		return nil, err
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}
	if buf.Len() >= maxAgentMarketplaceZipSize {
		return nil, fmt.Errorf("agent export exceeds marketplace size limit")
	}
	return buf.Bytes(), nil
}
