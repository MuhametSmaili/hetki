package update

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoUpdaterExplainsCompilerFailureWithoutReplacing(t *testing.T) {
	for _, verbose := range []bool{false, true} {
		t.Run(fmt.Sprintf("verbose=%t", verbose), func(t *testing.T) {
			bin := t.TempDir()
			script := fmt.Sprintf(`#!/bin/sh
if [ "$1 $2" = "mod download" ]; then
  printf '{"Path":"github.com/MuhametSmaili/hetki","Version":"v1.2.3","Sum":"h1:test","Origin":{"URL":"https://github.com/MuhametSmaili/hetki","Hash":"%s","Ref":"refs/tags/v1.2.3"}}\n'
  exit 0
fi
case " $* " in *" -v "*) printf '%%01024d\n' 0 >&2 ;; esac
printf 'main.go:12: undefined: missing\n' >&2
printf 'not-a-diagnostic\n'
exit 7
`, testCommit)
			require.NoError(t, os.WriteFile(filepath.Join(bin, "go"), []byte(script), 0755))
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			exe := filepath.Join(bin, "hetki")
			fakeBinary(t, exe, "v1.0.0")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			err := (&GoUpdater{exePath: exe, Verbose: verbose}).Update(ctx, testTarget("v1.2.3"))

			require.ErrorContains(t, err, "building source")
			assert.Contains(t, err.Error(), "main.go:12: undefined: missing")
			assert.NotContains(t, err.Error(), "not-a-diagnostic")
			var exitErr *exec.ExitError
			require.ErrorAs(t, err, &exitErr)
			assert.Equal(t, 7, exitErr.ExitCode())
			assert.Equal(t, "v1.0.0", readScriptVersion(t, exe))
			workspaces, globErr := filepath.Glob(filepath.Join(bin, ".hetki-source-*"))
			require.NoError(t, globErr)
			assert.Empty(t, workspaces)
		})
	}
}

func TestGoUpdaterExplainsStructuredDownloadFailure(t *testing.T) {
	bin := t.TempDir()
	script := `#!/bin/sh
printf '{"Error":"source unavailable","Other":"stdout-only"}\n'
exit 8
`
	require.NoError(t, os.WriteFile(filepath.Join(bin, "go"), []byte(script), 0755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	exe := filepath.Join(bin, "hetki")
	fakeBinary(t, exe, "v1.0.0")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := (&GoUpdater{exePath: exe}).Update(ctx, testTarget("v1.2.3"))

	require.ErrorContains(t, err, "verifying source origin")
	assert.Contains(t, err.Error(), "source unavailable")
	assert.NotContains(t, err.Error(), "stdout-only")
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 8, exitErr.ExitCode())
	assert.Equal(t, "v1.0.0", readScriptVersion(t, exe))
	workspaces, globErr := filepath.Glob(filepath.Join(bin, ".hetki-source-*"))
	require.NoError(t, globErr)
	assert.Empty(t, workspaces)
}

func TestGoUpdaterSanitizesRejectedSourceVersion(t *testing.T) {
	const secret = "fixture-private-version"
	for _, test := range []struct{ name, version, detail string }{
		{name: "credential", version: secret, detail: "[credentials omitted]"},
		{name: "oversized", version: strings.Repeat("λ", 400), detail: "[truncated]"},
	} {
		t.Run(test.name, func(t *testing.T) {
			bin := t.TempDir()
			metadata := fmt.Sprintf(`{"Path":"github.com/MuhametSmaili/hetki","Version":%q,"Query":"%s","Sum":"h1:test","Origin":{"URL":"https://github.com/MuhametSmaili/hetki","Hash":"%s"}}`, test.version, testCommit, testCommit)
			script := "#!/bin/sh\n[ \"$1 $2\" = \"mod download\" ] || exit 99\nprintf '%s\\n' '" + metadata + "'\n"
			require.NoError(t, os.WriteFile(filepath.Join(bin, "go"), []byte(script), 0755))
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("GH_TOKEN", secret)
			exe := filepath.Join(bin, "hetki")
			fakeBinary(t, exe, "v1.0.0")
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			err := (&GoUpdater{exePath: exe}).Update(ctx, testTarget(""))

			require.ErrorContains(t, err, "source version rejected")
			assert.Contains(t, err.Error(), test.detail)
			assert.NotContains(t, err.Error(), secret)
			assert.LessOrEqual(t, len(err.Error()), len("source version rejected: ")+512)
			assert.True(t, utf8.ValidString(err.Error()))
			assert.Equal(t, "v1.0.0", readScriptVersion(t, exe))
		})
	}
}

func TestCommandOutputKeepsStderrSeparateFromStructuredStdout(t *testing.T) {
	for _, exitCode := range []int{0, 9} {
		t.Run(fmt.Sprintf("exit=%d", exitCode), func(t *testing.T) {
			script := `printf '{"result":"stdout-only"}'; printf 'helper explanation\n' >&2; exit "$1"`

			out, err := commandOutput(context.Background(), 5*time.Second, 1024, "sh", "-c", script, "_", fmt.Sprint(exitCode))

			assert.JSONEq(t, `{"result":"stdout-only"}`, string(out))
			if exitCode == 0 {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, "helper explanation")
			assert.NotContains(t, err.Error(), "stdout-only")
			var exitErr *exec.ExitError
			require.ErrorAs(t, err, &exitErr)
			assert.Equal(t, exitCode, exitErr.ExitCode())
		})
	}
}

func TestCommandFailureDiagnosticsOmitCredentialsAndControls(t *testing.T) {
	t.Setenv("GH_TOKEN", "primary-secret")
	t.Setenv("GITHUB_TOKEN", "fallback-secret")
	t.Setenv("HETKI_TEST_DIAGNOSTIC", "compiler: \x1b[31mmissing\x1b[0m\tname\a\n"+
		"primary-\x1b[31msecret\x1b[0m\nfallback-secret\n"+
		"Authorization: Bearer private-header\nAPI_KEY=other-secret\n\"api-key\":\"json-secret\"\n"+
		"\"api_key\"\t:\"tab-secret\"\npass\x1b[31mword\x1b[0m\t=mixed-secret\n"+
		"fetch https://user:url-password@example.test/file?signature=query-secret failed\n")

	_, err := commandOutput(context.Background(), 5*time.Second, 1024, "sh", "-c",
		`printf '%s' "$HETKI_TEST_DIAGNOSTIC" >&2; exit 7`)

	require.ErrorContains(t, err, "compiler: missing")
	assert.Contains(t, err.Error(), "[credentials omitted]")
	assert.Contains(t, err.Error(), "[URL omitted]")
	for _, unsafe := range []string{"primary-secret", "fallback-secret", "private-header",
		"other-secret", "json-secret", "tab-secret", "mixed-secret", "url-password", "query-secret", "\x1b", "\t", "\a", "\n"} {
		assert.NotContains(t, err.Error(), unsafe)
	}
}

func TestCommandFailureDiagnosticBounds(t *testing.T) {
	for _, test := range []struct{ name, script, diagnostic string }{
		{
			name:       "display",
			script:     `printf '%s' "$HETKI_TEST_DIAGNOSTIC" >&2; exit 9`,
			diagnostic: strings.Repeat("λ", 400) + "\nuseful failure",
		},
		{
			name:   "capture drops incomplete line",
			script: `printf 'useful failure\nprivate-fragment%01048576d' 0 >&2; exit 9`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("GH_TOKEN", "")
			t.Setenv("GITHUB_TOKEN", "")
			t.Setenv("HETKI_TEST_DIAGNOSTIC", test.diagnostic)

			_, err := commandOutput(context.Background(), 5*time.Second, 1024, "sh", "-c", test.script)

			require.ErrorContains(t, err, "useful failure")
			assert.Contains(t, err.Error(), "[truncated]")
			assert.NotContains(t, err.Error(), "private-fragment")
			assert.True(t, utf8.ValidString(err.Error()))
			var exitErr *exec.ExitError
			require.ErrorAs(t, err, &exitErr)
			prefix := exitErr.Error() + ": "
			require.True(t, strings.HasPrefix(err.Error(), prefix))
			assert.LessOrEqual(t, len(strings.TrimPrefix(err.Error(), prefix)), 512)
		})
	}
}

func TestCommandOutputOmitsWaitDelayFragments(t *testing.T) {
	for _, exitCode := range []int{0, 9} {
		t.Run(fmt.Sprintf("exit=%d", exitCode), func(t *testing.T) {
			t.Setenv("GH_TOKEN", "partial-secret")
			fifo := filepath.Join(t.TempDir(), "ready")
			script := `mkfifo "$1"
(printf 'useful failure\npartial-' >&2; printf 'ready\n' > "$1"; sleep 10; printf 'secret\n' >&2) &
read -r ready < "$1"
exit "$2"`

			_, err := commandOutput(context.Background(), 5*time.Second, 1024, "sh", "-c", script, "_", fifo, fmt.Sprint(exitCode))

			require.ErrorContains(t, err, "useful failure")
			assert.NotContains(t, err.Error(), "partial-")
			assert.Contains(t, err.Error(), "[truncated]")
			if exitCode == 0 {
				require.ErrorIs(t, err, exec.ErrWaitDelay)
				return
			}
			var exitErr *exec.ExitError
			require.ErrorAs(t, err, &exitErr)
			assert.Equal(t, exitCode, exitErr.ExitCode())
		})
	}
}

func TestCommandOutputRejectsOversizedStdout(t *testing.T) {
	_, err := commandOutput(context.Background(), 5*time.Second, 64, "sh", "-c", "printf '%01024d' 0")

	require.ErrorContains(t, err, "command output exceeds 64 bytes")
}
