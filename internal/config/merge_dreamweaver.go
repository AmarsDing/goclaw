package config

import (
	"encoding/json"
	"os"
)

// MergeDeepJSON merges JSON object maps; keys in `over` replace `base`, nested objects merge recursively.
func MergeDeepJSON(base, over map[string]any) map[string]any {
	if base == nil {
		base = make(map[string]any)
	}
	out := make(map[string]any, len(base)+len(over))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		if om, ok := v.(map[string]any); ok {
			if bv, ok := out[k].(map[string]any); ok {
				out[k] = MergeDeepJSON(bv, om)
			} else {
				out[k] = MergeDeepJSON(nil, om)
			}
		} else {
			out[k] = v
		}
	}
	return out
}

// MergeDreamWeaverConfig merges overlays onto base (later keys win). Nil inputs are handled safely.
func MergeDreamWeaverConfig(base, over *DreamWeaverConfig) *DreamWeaverConfig {
	if base == nil {
		return over
	}
	if over == nil {
		return base
	}
	b, err := json.Marshal(base)
	if err != nil {
		return base
	}
	o, err := json.Marshal(over)
	if err != nil {
		return base
	}
	var bm, om map[string]any
	if err := json.Unmarshal(b, &bm); err != nil {
		return base
	}
	if err := json.Unmarshal(o, &om); err != nil {
		return base
	}
	merged := MergeDeepJSON(bm, om)
	raw, err := json.Marshal(merged)
	if err != nil {
		return base
	}
	var out DreamWeaverConfig
	if err := json.Unmarshal(raw, &out); err != nil {
		return base
	}
	return &out
}

// MergeChain applies configs in order: later layers win (deep merge).
func MergeChain(layers ...*DreamWeaverConfig) *DreamWeaverConfig {
	var acc *DreamWeaverConfig
	for _, layer := range layers {
		if layer == nil {
			continue
		}
		acc = MergeDreamWeaverConfig(acc, layer)
	}
	return acc
}

// LoadDreamWeaverFile reads `.goclaw/dreamweaver.json` style config from an absolute path.
func LoadDreamWeaverFile(path string) (*DreamWeaverConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c DreamWeaverConfig
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}
