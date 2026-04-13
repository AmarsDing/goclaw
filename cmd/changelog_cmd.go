package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/changelog"
)

func changelogCmd() *cobra.Command {
	var from, to, repo string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "changelog",
		Short: "Generate a Markdown changelog from git (goclaw repo tracking)",
		Long: `Compares two refs with git log and git diff --name-status, groups paths by subsystem
(configured in internal/changelog/subsystems.yaml), and prints Markdown to stdout.

Environment: BASE_REF and HEAD_REF override --from / --to when set.`,
		Run: func(cmd *cobra.Command, args []string) {
			if v := os.Getenv("BASE_REF"); v != "" {
				from = v
			}
			if v := os.Getenv("HEAD_REF"); v != "" {
				to = v
			}
			rep, md, err := changelog.Generate(changelog.ReportOptions{
				RepoRoot: repo,
				FromRef:  from,
				ToRef:    to,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "changelog: %v\n", err)
				os.Exit(1)
			}
			if asJSON {
				raw, err := changelog.ReportJSON(rep)
				if err != nil {
					fmt.Fprintf(os.Stderr, "changelog: %v\n", err)
					os.Exit(1)
				}
				fmt.Println(string(raw))
				return
			}
			fmt.Print(strings.TrimSpace(md) + "\n")
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "start ref (default: HEAD~1)")
	cmd.Flags().StringVar(&to, "to", "HEAD", "end ref")
	cmd.Flags().StringVar(&repo, "repo", "", "repository root (default: current directory)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON instead of Markdown")
	return cmd
}
