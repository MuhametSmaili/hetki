package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStatusDaemonCommandIsPrivateAndRequiresDescriptor(t *testing.T) {
	command := newStatusDaemonCommand()
	command.SetArgs([]string{"server-without-descriptor"})
	command.SilenceUsage = true
	command.SilenceErrors = true

	err := command.Execute()

	require.True(t, command.Hidden)
	require.ErrorContains(t, err, "accepts 2 arg(s)")
}
