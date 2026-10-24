//go:build integration && (darwin || linux)

package tmux

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/foreground"
	"github.com/stretchr/testify/require"
)

func TestQueryStateRefinesGenericForegroundNodeTitle(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is required for the foreground naming fixture")
	}
	b := newIsolatedTmuxBackend(t)
	_, err = b.client.Run(context.Background(), "new-session", "-d", "-s", "program", node, "-e", "process.title='unlisted-cli'; setInterval(()=>{},1000)")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		state, err := b.QueryState(context.Background())
		if err != nil || len(state.Sessions) != 1 {
			return false
		}
		pane := state.Sessions[0].Windows[0].Panes[0]
		if runtime.GOOS == "darwin" {
			return pane.Command == "node" && pane.Program == "unlisted-cli"
		}
		return pane.Command == "unlisted-cli" && pane.Program == ""
	}, 3*time.Second, 20*time.Millisecond)

	// A title is only a naming hint; neither the tmux window nor raw command
	// is rewritten, and an ordinary Node process remains raw Node.
	_, err = b.client.Run(context.Background(), "respawn-pane", "-k", "-t", "program:", node, "-e", "setInterval(()=>{},1000)")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		state, err := b.QueryState(context.Background())
		if err != nil || len(state.Sessions) != 1 {
			return false
		}
		pane := state.Sessions[0].Windows[0].Panes[0]
		return pane.Command == "node" && pane.Program == ""
	}, 3*time.Second, 20*time.Millisecond)
}

func TestForegroundRejectsEmptyArgvZero(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("native argv inspection is supported only on Darwin")
	}
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("a C compiler is required for the empty argv-zero fixture")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "fixture.c")
	binary := filepath.Join(dir, "fixture")
	ready := filepath.Join(dir, "ready")
	// Exec the same single-process foreground job with an empty argv-zero
	// followed by a name-looking argument. Skipping NULs would expose it.
	require.NoError(t, os.WriteFile(source, []byte(`#include <stdio.h>
#include <unistd.h>
int main(int argc, char **argv) {
    if (argc == 2) {
        char *args[] = {"", "unlisted-secret", argv[1], NULL};
        execv(argv[0], args);
        return 1;
    }
    if (argc != 3 || argv[0][0] != 0) return 2;
    FILE *f = fopen(argv[2], "w");
    if (!f) return 3;
    fputs("ready", f);
    fclose(f);
    for (;;) pause();
}
`), 0600))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, cc, source, "-o", binary).CombinedOutput()
	require.NoError(t, err, "%s", out)
	b := newIsolatedTmuxBackend(t)
	_, err = b.client.Run(ctx, "new-session", "-d", "-s", "empty", binary, ready)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		_, err := os.Stat(ready)
		return err == nil
	}, 3*time.Second, 20*time.Millisecond)
	observed, err := RunQuery(ctx, b.client, LoadStateQuery{IncludeProgramInfo: true})
	require.NoError(t, err)
	require.Empty(t, foreground.Name(ctx, observed.Sessions[0].Windows[0].Panes[0].PID), "never expose a later argument as the process name")
}

func TestQueryStateFallsBackForOversizedDarwinProcessData(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("native argv inspection is supported only on Darwin")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is required for the oversized process data fixture")
	}
	for _, kind := range []string{"arguments", "environment"} {
		t.Run(kind, func(t *testing.T) {
			padding := strings.Repeat("x", 80*1024)
			if kind == "environment" {
				t.Setenv("FOREGROUND_FIXTURE_PADDING", padding)
			}
			dir := t.TempDir()
			script := filepath.Join(dir, "fixture.js")
			ready := filepath.Join(dir, "ready")
			require.NoError(t, os.WriteFile(script, fmt.Appendf(nil, "process.title='unlisted-cli';require('fs').writeFileSync(%q,'ready');setInterval(()=>{},1000)", ready), 0600))
			args := []string{"new-session", "-d", "-s", "oversized", node, script}
			if kind == "arguments" {
				// Keep tmux's command message small; expand the large argument
				// locally before exec so the fixture remains one foreground process.
				payload := filepath.Join(dir, "padding")
				launcher := filepath.Join(dir, "launch.sh")
				require.NoError(t, os.WriteFile(payload, []byte(padding), 0600))
				require.NoError(t, os.WriteFile(launcher, fmt.Appendf(nil, "#!/bin/sh\nexec %q %q \"$(cat %q)\"\n", node, script, payload), 0700))
				args = []string{"new-session", "-d", "-s", "oversized", launcher}
			}
			b := newIsolatedTmuxBackend(t)
			_, err := b.client.Run(context.Background(), args...)
			require.NoError(t, err)
			require.Eventually(t, func() bool {
				_, err := os.Stat(ready)
				return err == nil
			}, 3*time.Second, 20*time.Millisecond)
			state, err := b.QueryState(context.Background())
			require.NoError(t, err)
			require.Equal(t, "node", state.Sessions[0].Windows[0].Panes[0].Command)
			require.Empty(t, state.Sessions[0].Windows[0].Panes[0].Program)
		})
	}
}

func TestQueryStateNamesOnlyForegroundLeader(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is required for the foreground naming fixture")
	}
	b := newIsolatedTmuxBackend(t)
	dir := t.TempDir()
	script := filepath.Join(dir, "node_modules", "unlisted-cli", "dist", "cli.js")
	ready := filepath.Join(dir, "ready")
	childReady := filepath.Join(dir, "child-ready")
	require.NoError(t, os.MkdirAll(filepath.Dir(script), 0755))
	require.NoError(t, os.WriteFile(script, fmt.Appendf(nil, "require('fs').writeFileSync(%q,'ready'); setInterval(()=>{},1000)", ready), 0600))
	_, err = b.client.Run(context.Background(), "new-session", "-d", "-s", "entry", node, "--no-warnings", script)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		_, err := os.Stat(ready)
		return err == nil
	}, 3*time.Second, 20*time.Millisecond)
	state, err := b.QueryState(context.Background())
	require.NoError(t, err)
	require.Equal(t, "node", state.Sessions[0].Windows[0].Panes[0].Command)
	require.Empty(t, state.Sessions[0].Windows[0].Panes[0].Program)

	// A helper process does not replace the foreground group's leader.
	require.NoError(t, os.WriteFile(script, fmt.Appendf(nil, "process.title='unlisted-cli'; const child=require('child_process').spawn('sleep',['60'],{stdio:'inherit'}); child.on('spawn',()=>require('fs').writeFileSync(%q,'ready')); setInterval(()=>{},1000)", childReady), 0600))
	_, err = b.client.Run(context.Background(), "respawn-pane", "-k", "-t", "entry:", node, script)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		_, err := os.Stat(childReady)
		return err == nil
	}, 3*time.Second, 20*time.Millisecond)
	observed, err := RunQuery(context.Background(), b.client, LoadStateQuery{IncludeProgramInfo: true})
	require.NoError(t, err)
	name := foreground.Name(context.Background(), observed.Sessions[0].Windows[0].Panes[0].PID)
	state, err = b.QueryState(context.Background())
	require.NoError(t, err)
	pane := state.Sessions[0].Windows[0].Panes[0]

	if runtime.GOOS == "darwin" {
		require.Equal(t, "unlisted-cli", name)
		require.Equal(t, "node", pane.Command)
		require.Equal(t, "unlisted-cli", pane.Program)
		return
	}
	require.Empty(t, name)
	require.Equal(t, "unlisted-cli", pane.Command)
	require.Empty(t, pane.Program)
}
