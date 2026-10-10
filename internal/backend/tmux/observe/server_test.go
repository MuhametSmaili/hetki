package observe

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type queryRunner func(context.Context, int, ...string) (string, error)

func (run queryRunner) RunLimited(ctx context.Context, limit int, args ...string) (string, error) {
	return run(ctx, limit, args...)
}

func TestServerDiscoveryRejectsIncarnationChanges(t *testing.T) {
	cases := []struct {
		name   string
		rows   [2]string
		births [2]string
		want   error
	}{
		{
			name:   "stable escaped socket",
			rows:   [2]string{"/tmp/a\\|b\\ c.sock|42|123|3.8\n", "/tmp/a\\|b\\ c.sock|42|123|3.8\n"},
			births: [2]string{"birth", "birth"},
		},
		{
			name:   "reused PID",
			rows:   [2]string{"/tmp/sock|42|123|3.8\n", "/tmp/sock|42|123|3.8\n"},
			births: [2]string{"before", "after"},
			want:   ErrServerChanged,
		},
		{
			name:   "replaced socket server",
			rows:   [2]string{"/tmp/sock|42|123|3.8\n", "/tmp/sock|43|124|3.8\n"},
			births: [2]string{"birth", "birth"},
			want:   ErrServerChanged,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			queries, reads := 0, 0
			runner := queryRunner(func(context.Context, int, ...string) (string, error) {
				row := tc.rows[queries]
				queries++
				return row, nil
			})
			birth := func(context.Context, int) (string, error) {
				value := tc.births[reads]
				reads++
				return value, nil
			}

			server, err := resolveServer(t.Context(), runner, birth)

			require.ErrorIs(t, err, tc.want)
			require.Equal(t, 2, queries)
			require.Equal(t, 2, reads)
			if tc.want == nil {
				require.Equal(t, "/tmp/a|b c.sock", server.Socket)
				require.Equal(t, "birth", server.Birth)
			}
		})
	}
}

func TestServerIdentityIgnoresSocketAliases(t *testing.T) {
	server := Server{Socket: "/tmp/sock", PID: 42, Started: 123, Birth: "birth", Version: "3.8"}
	alias := server
	alias.Socket = "/private/tmp/sock"
	replacement := server
	replacement.Birth = "new-birth"

	key, aliasKey, replacementKey := server.Identity(), alias.Identity(), replacement.Identity()

	require.Equal(t, key, aliasKey)
	require.NotEqual(t, key, replacementKey)
}

func TestServerValidationHonorsCancellationAndBirth(t *testing.T) {
	birth, err := readProcessBirth(t.Context(), os.Getpid())
	require.NoError(t, err)
	server := Server{Socket: "/tmp/unused", PID: os.Getpid(), Started: 123, Birth: birth, Version: "3.8"}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	replacement := server
	replacement.Birth = "different-birth"

	validErr := server.Validate(t.Context())
	cancelErr := server.Validate(ctx)
	replacedErr := replacement.Validate(t.Context())

	require.NoError(t, validErr)
	require.ErrorIs(t, cancelErr, context.Canceled)
	require.ErrorIs(t, replacedErr, ErrServerChanged)
}

func TestServerDiscoveryPreservesNativeFailure(t *testing.T) {
	failure := errors.New("native lookup failed")
	runner := queryRunner(func(context.Context, int, ...string) (string, error) {
		return "/tmp/sock|42|123|3.8\n", nil
	})

	_, err := resolveServer(t.Context(), runner, func(context.Context, int) (string, error) {
		return "", failure
	})

	require.ErrorIs(t, err, failure)
}

func TestLinuxBirthParserHandlesNamesAndRejectsDeadProcesses(t *testing.T) {
	cases := []struct {
		name  string
		state string
		pid   int
		valid bool
	}{
		{"parentheses in name", "S", 42, true},
		{"wrong process", "S", 43, false},
		{"zombie", "Z", 42, false},
		{"exiting", "X", 42, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := "42 (command ) with ( spaces) " + tc.state + " " + strings.Repeat("0 ", 18) + "9876"

			info, err := parseProcessStat(data, tc.pid)

			if !tc.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "9876", info.Birth)
		})
	}
}
