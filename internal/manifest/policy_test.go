package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func acceptLayout(string) error { return nil }

func TestValidationErrorsRetainLaterCausesAndOwnIssueMetadata(t *testing.T) {
	denied := errors.New("unsupported layout")
	issues := []ValidationError{
		{Field: "sessions[0].name", Message: "bad name"},
		{Field: "sessions[0].windows[0].layout", Message: "bad layout", Cause: denied},
	}

	err := ToError(issues)
	issues[1] = ValidationError{Message: "replacement"}

	require.ErrorIs(t, err, denied)
	require.EqualError(t, err, "workspace validation failed:\n  - sessions[0].name: bad name\n  - sessions[0].windows[0].layout: bad layout")
}

func TestManifestOperationsPreserveLayoutPolicyErrors(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	denied := errors.New("unsupported opaque layout")
	workspace := &Workspace{Sessions: []Session{{Name: "dev", Windows: []Window{{Name: "editor", Layout: "opaque"}}}}}
	data, err := yaml.Marshal(workspace)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	require.NoError(t, os.WriteFile(path, data, 0600))

	for _, tc := range []struct {
		name   string
		policy func(string) error
		want   error
	}{
		{"required policy", nil, ErrLayoutValidatorRequired},
		{"policy rejection", func(text string) error {
			if text != "opaque" {
				return errors.New("unexpected layout")
			}
			return denied
		}, denied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, operation := range []struct {
				name string
				run  func() error
			}{
				{"validate", func() error { return ToError(Validate(workspace, tc.policy)) }},
				{"parse", func() error {
					_, err := Parse(data, path, tc.policy)
					return err
				}},
				{"marshal", func() error {
					_, err := Marshal(workspace, path, tc.policy)
					return err
				}},
				{"load", func() error {
					_, err := NewFileLoader(path, tc.policy).Load()
					return err
				}},
			} {
				t.Run(operation.name, func(t *testing.T) {
					err := operation.run()

					require.ErrorIs(t, err, tc.want)
					if tc.policy != nil {
						var issue ValidationError
						require.ErrorAs(t, err, &issue)
						require.Equal(t, "sessions[0].windows[0].layout", issue.Field)
					}
				})
			}
		})
	}
}

func TestValidateSkipsPolicyForOmittedOrOversizedLayouts(t *testing.T) {
	for _, tc := range []struct {
		name, layout string
		wantError    bool
	}{
		{"omitted", "", false},
		{"oversized", strings.Repeat("x", MaxPathLength+1), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace := &Workspace{Sessions: []Session{{Name: "dev", Windows: []Window{{Layout: tc.layout}}}}}
			policy := func(string) error {
				t.Fatal("unexpected layout parsing")
				return nil
			}

			errs := Validate(workspace, policy)

			if tc.wantError {
				require.ErrorContains(t, ToError(errs), "layout is too long")
			} else {
				require.Empty(t, errs)
			}
		})
	}
}
