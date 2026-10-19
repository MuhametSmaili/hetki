package daemon

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/testenv"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestStopPreservesUnsafeLockFiles(t *testing.T) {
	for _, tc := range []struct {
		name   string
		create func(string, string) error
	}{
		{"permissions", func(path, _ string) error { return os.WriteFile(path, nil, 0644) }},
		{"symlink", func(path, target string) error { return os.Symlink(target, path) }},
		{"hardlink", func(path, target string) error { return os.Link(target, path) }},
		{"directory", func(path, _ string) error { return os.Mkdir(path, 0700) }},
		{"fifo", func(path, _ string) error { return unix.Mkfifo(path, 0600) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testenv.PrivateHome(t)
			client, err := Connect("unsafe-lock")
			require.NoError(t, err)
			path := client.path + ".lock"
			target := client.path + ".target"
			require.NoError(t, os.WriteFile(target, []byte("untouched"), 0600))
			require.NoError(t, tc.create(path, target))
			before, err := os.Lstat(path)
			require.NoError(t, err)

			_, err = Stop(t.Context(), client.server)
			after, statErr := os.Lstat(path)
			data, readErr := os.ReadFile(target)

			require.Error(t, err)
			require.NoError(t, statErr)
			require.True(t, os.SameFile(before, after))
			require.Equal(t, before.Mode(), after.Mode())
			require.NoError(t, readErr)
			require.Equal(t, "untouched", string(data))
		})
	}
}

func TestCleanupWaitDoesNotWaitForReplacementOwner(t *testing.T) {
	client := fixtureClient(t, "owner-handoff")
	original, err := lockEndpoint(client.path)
	require.NoError(t, err)
	require.NotNil(t, original)
	closeOriginal := sync.OnceValue(original.Close)
	t.Cleanup(func() { require.NoError(t, closeOriginal()) })
	require.NoError(t, original.setOwner(strings.Repeat("a", 32)))
	observer, err := openEndpointLock(client.path, 0)
	require.NoError(t, err)
	defer observer.Close()
	require.NoError(t, closeOriginal())
	replacement, err := lockEndpoint(client.path)
	require.NoError(t, err)
	require.NotNil(t, replacement)
	defer replacement.Close()
	require.NoError(t, replacement.setOwner(strings.Repeat("b", 32)))
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	err = observer.waitStopped(ctx, strings.Repeat("a", 32))

	require.NoError(t, err)
	owned, err := endpointOwned(client.path)
	require.NoError(t, err)
	require.True(t, owned, "the replacement must retain ownership")
}

func TestCleanupWaitRejectsReplacedLockPath(t *testing.T) {
	client := fixtureClient(t, "lock-replacement")
	original, err := lockEndpoint(client.path)
	require.NoError(t, err)
	require.NotNil(t, original)
	defer original.Close()
	observer, err := openEndpointLock(client.path, 0)
	require.NoError(t, err)
	defer observer.Close()
	require.NoError(t, os.Rename(client.path+".lock", client.path+".old-lock"))
	replacement, err := lockEndpoint(client.path)
	require.NoError(t, err)
	require.NotNil(t, replacement)
	defer replacement.Close()

	err = observer.waitStopped(t.Context(), strings.Repeat("a", 32))

	require.Error(t, err)
	owned, err := endpointOwned(client.path)
	require.NoError(t, err)
	require.True(t, owned)
}

func TestCleanupWaitDoesNotTreatIncompleteOwnerAsCompletion(t *testing.T) {
	client := fixtureClient(t, "incomplete-owner")
	owner, err := lockEndpoint(client.path)
	require.NoError(t, err)
	require.NotNil(t, owner)
	defer owner.Close()
	require.NoError(t, owner.setOwner("a"))
	observer, err := openEndpointLock(client.path, 0)
	require.NoError(t, err)
	defer observer.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	err = observer.waitStopped(ctx, strings.Repeat("a", 32))

	require.ErrorIs(t, err, context.DeadlineExceeded)
}
