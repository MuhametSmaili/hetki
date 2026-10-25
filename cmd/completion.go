package cmd

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/MuhametSmaili/hetki/internal/manifest"
	"github.com/MuhametSmaili/hetki/internal/terminal"
	"github.com/spf13/cobra"
)

func getWorkspaceNames() ([]string, error) {
	configDir, err := manifest.GetConfigDir()
	if err != nil {
		return nil, err
	}
	paths, err := manifest.ScanWorkspaces(filepath.Join(configDir, "workspaces"))
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(paths))
	for name := range paths {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func completeWorkspaceNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 || cmd.Flags().Changed("file") {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	names, err := getWorkspaceNames()
	if err != nil {
		cobra.CompErrorln("complete workspace names: " + terminal.Sanitize(err.Error()))
		return nil, cobra.ShellCompDirectiveError | cobra.ShellCompDirectiveNoFileComp
	}
	completions := names[:0]
	for _, name := range names {
		if !strings.HasPrefix(name, toComplete) {
			continue
		}
		// Cobra routes dash-prefixed tokens here only after --, not as flags.
		if toComplete == "" && strings.HasPrefix(name, "-") {
			continue
		}
		completions = append(completions, name)
	}
	return completions, cobra.ShellCompDirectiveNoFileComp
}
