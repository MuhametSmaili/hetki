package update

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type updateRoundTripFunc func(*http.Request) (*http.Response, error)

func (f updateRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// stubUpdateHTTP preserves request origins and http.Client redirect behavior
// without sending credentials or requests to a real service.
func stubUpdateHTTP(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	previous := http.DefaultTransport
	http.DefaultTransport = updateRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response.Result(), nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
}

func TestAPIRedirectDoesNotForwardOrRestoreCredentials(t *testing.T) {
	t.Setenv("GH_TOKEN", "api-token")
	t.Setenv("GITHUB_TOKEN", "")
	// Allow a subdomain to exercise credential scope independently of egress
	// policy: net/http normally forwards authorization to subdomains.
	previous := allowedRedirectHosts
	allowedRedirectHosts = map[string]bool{"api.github.com": true, "downloads.api.github.com": true}
	t.Cleanup(func() { allowedRedirectHosts = previous })
	var authorizations []string
	stubUpdateHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		switch len(authorizations) {
		case 1:
			http.Redirect(w, r, "https://downloads.api.github.com/asset", http.StatusFound)
		case 2:
			http.Redirect(w, r, "https://api.github.com/return", http.StatusFound)
		case 3:
			http.Redirect(w, r, "/final", http.StatusFound)
		default:
			_, _ = w.Write([]byte(`{"tag_name":"v1.2.3"}`))
		}
	})

	tag, err := ResolveTarget(context.Background(), Options{TargetVersion: "v1.2.3"})

	require.NoError(t, err)
	assert.Equal(t, "v1.2.3", tag)
	assert.Equal(t, []string{"Bearer api-token", "", "", ""}, authorizations)
}

func TestAPITokenRequiresCanonicalHTTPSOrigin(t *testing.T) {
	for _, test := range []struct{ name, origin string }{
		{name: "insecure API", origin: "http://api.github.com"},
		{name: "different port", origin: "https://api.github.com:8443"},
		{name: "subdomain", origin: "https://downloads.api.github.com"},
		{name: "lookalike hostname", origin: "https://api.github.com.example.test"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("GH_TOKEN", "api-token")
			t.Setenv("GITHUB_TOKEN", "")
			previous := apiBase
			apiBase = test.origin + "/"
			t.Cleanup(func() { apiBase = previous })
			var authorizations []string
			stubUpdateHTTP(t, func(w http.ResponseWriter, r *http.Request) {
				authorizations = append(authorizations, r.Header.Get("Authorization"))
				_, _ = w.Write([]byte(`{"tag_name":"v1.2.3"}`))
			})

			tag, err := ResolveTarget(context.Background(), Options{TargetVersion: "v1.2.3"})

			require.NoError(t, err)
			assert.Equal(t, "v1.2.3", tag)
			assert.Equal(t, []string{""}, authorizations)
		})
	}
}

func TestAuthenticatedAPIRedirects(t *testing.T) {
	for _, test := range []struct {
		name, location, authorization string
		rejected                      bool
	}{
		{name: "same origin", location: "/redirected", authorization: "Bearer api-token"},
		{name: "release page", location: "https://github.com/redirected"},
		{name: "object CDN", location: "https://objects.githubusercontent.com/redirected"},
		{name: "release CDN", location: "https://release-assets.githubusercontent.com/redirected"},
		{name: "unknown host", location: "https://example.test/redirected", rejected: true},
		{name: "HTTP downgrade", location: "http://api.github.com/redirected", rejected: true},
		{name: "different port", location: "https://api.github.com:8443/redirected", rejected: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("GH_TOKEN", "api-token")
			t.Setenv("GITHUB_TOKEN", "")
			var authorizations []string
			stubUpdateHTTP(t, func(w http.ResponseWriter, r *http.Request) {
				authorizations = append(authorizations, r.Header.Get("Authorization"))
				if len(authorizations) == 1 {
					http.Redirect(w, r, test.location, http.StatusFound)
					return
				}
				_, _ = w.Write([]byte(`{"tag_name":"v1.2.3"}`))
			})

			tag, err := ResolveTarget(context.Background(), Options{TargetVersion: "v1.2.3"})

			if test.rejected {
				require.Error(t, err)
				assert.NotContains(t, err.Error(), "api-token")
				assert.Equal(t, []string{"Bearer api-token"}, authorizations)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "v1.2.3", tag)
			assert.Equal(t, []string{"Bearer api-token", test.authorization}, authorizations)
		})
	}
}

func TestReleaseDownloadsStayAnonymous(t *testing.T) {
	t.Setenv("GH_TOKEN", "api-token")
	t.Setenv("GITHUB_TOKEN", "")
	content := []byte("release artifact")
	digest := sha256.Sum256(content)
	file, err := os.CreateTemp(t.TempDir(), "artifact")
	require.NoError(t, err)
	var authorizations []string
	stubUpdateHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/binary":
			http.Redirect(w, r, "https://api.github.com/binary-final", http.StatusFound)
		case "/binary-final":
			_, _ = w.Write(content)
		case "/checksums":
			_, _ = fmt.Fprintf(w, "%x  hetki\n", digest)
		default:
			t.Errorf("unexpected download request: %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	err = downloadToFile(context.Background(), "https://github.com/binary", file, 1<<20)
	require.NoError(t, err)
	verifiedDigest, err := verifyBinaryChecksum(context.Background(), "https://github.com/checksums", file.Name(), "hetki")

	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("%x", digest), verifiedDigest)
	assert.Equal(t, []string{"", "", ""}, authorizations)
}

func TestRejectedAPITokenFailsWithoutRetryOrDisclosure(t *testing.T) {
	t.Setenv("GH_TOKEN", "rejected-token")
	t.Setenv("GITHUB_TOKEN", "fallback-token")
	requests := 0
	stubUpdateHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintf(w, "rejected %s", r.Header.Get("Authorization"))
	})

	_, err := ResolveTarget(context.Background(), Options{TargetVersion: "v1.2.3"})

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "rejected-token")
	assert.NotContains(t, err.Error(), "fallback-token")
	assert.Equal(t, 1, requests)
}

func TestResolveTargetUsesEnvironmentToken(t *testing.T) {
	for _, test := range []struct {
		name, ghToken, githubToken, authorization string
	}{
		{name: "GH_TOKEN takes precedence", ghToken: "primary-token", githubToken: "fallback-token", authorization: "Bearer primary-token"},
		{name: "empty GH_TOKEN uses GITHUB_TOKEN", githubToken: "fallback-token", authorization: "Bearer fallback-token"},
		{name: "no token stays anonymous"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("GH_TOKEN", test.ghToken)
			t.Setenv("GITHUB_TOKEN", test.githubToken)
			var authorization string
			stubUpdateHTTP(t, func(w http.ResponseWriter, r *http.Request) {
				authorization = r.Header.Get("Authorization")
				assert.Equal(t, "https://api.github.com/repos/MuhametSmaili/hetki/releases/tags/v1.2.3", r.URL.String())
				assert.Equal(t, "application/vnd.github+json", r.Header.Get("Accept"))
				_, _ = w.Write([]byte(`{"tag_name":"v1.2.3"}`))
			})

			tag, err := ResolveTarget(context.Background(), Options{TargetVersion: "v1.2.3"})

			require.NoError(t, err)
			assert.Equal(t, "v1.2.3", tag)
			assert.Equal(t, test.authorization, authorization)
		})
	}
}
