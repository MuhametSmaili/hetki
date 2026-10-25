package cmd

import (
	"path/filepath"
	"sort"

	"github.com/MuhametSmaili/hetki/internal/manifest"
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

	names, _ := getWorkspaceNames()
	return names, cobra.ShellCompDirectiveNoFileComp
}
