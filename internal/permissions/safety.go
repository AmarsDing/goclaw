package permissions

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
)

// dangerousPathPatterns are bypass-immune: high-risk paths even when modes or rules would allow access.
var dangerousPathPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(^|[/\\])\.env(\.|$)`),
	regexp.MustCompile(`(?i)[/\\]\.ssh[/\\]`),
	regexp.MustCompile(`(?i)[/\\]\.gnupg[/\\]`),
	regexp.MustCompile(`(?i)[/\\]\.aws[/\\](credentials|config)(\.[^/\\]*)?$`),
	regexp.MustCompile(`(?i)[/\\](id_rsa|id_ed25519)$`),
	regexp.MustCompile(`(?i)[/\\][^/\\]+\.(pem|key)$`),
	regexp.MustCompile(`(?i)/etc/(passwd|shadow|sudoers)`),
	regexp.MustCompile(`(?i)[/\\]\.kube[/\\]config$`),
}

// dangerousExecPatterns block obviously unsafe shell pipelines or destructive ops.
var dangerousExecPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bcurl\b.*\|\s*(ba)?sh`),
	regexp.MustCompile(`(?i)\bwget\b.*\|\s*(ba)?sh`),
	regexp.MustCompile(`(?i)\bchmod\b.*\+s\b`),
	regexp.MustCompile(`(?i)\bsudo\b`),
	regexp.MustCompile(`(?i)\bdd\b.*\bof=/dev/`),
	regexp.MustCompile(`(?i)\bmkfs\b`),
	regexp.MustCompile(`(?i)\b(rm|del)\b.*(-rf|/s|/q).*(/|\\)(etc|usr|windows|system32)`),
}

func normalizePathArg(p string) string {
	return filepath.ToSlash(strings.TrimSpace(p))
}

// DefaultSafetyChecker returns a SafetyChecker that enforces bypass-immune rules on
// file path arguments (read/write tools) and shell commands (exec family).
func DefaultSafetyChecker() SafetyChecker {
	return func(ctx context.Context, req PermissionRequest, class *SecurityClassification) (*SafetyVerdict, error) {
		_ = ctx
		_ = class
		for _, key := range []string{"path", "file_path", "file", "target"} {
			val, ok := req.Arguments[key].(string)
			if !ok || val == "" {
				continue
			}
			norm := normalizePathArg(val)
			for _, pat := range dangerousPathPatterns {
				if pat.MatchString(norm) {
					return &SafetyVerdict{
						Action: ActionDeny,
						Reason: "bypass-immune: access to sensitive path " + val,
					}, nil
				}
			}
		}
		switch req.ToolName {
		case "exec", "shell", "bash":
			cmd, _ := req.Arguments["command"].(string)
			for _, pat := range dangerousExecPatterns {
				if pat.MatchString(cmd) {
					return &SafetyVerdict{
						Action: ActionDeny,
						Reason: "bypass-immune: dangerous command pattern blocked",
					}, nil
				}
			}
		}
		return nil, nil
	}
}
