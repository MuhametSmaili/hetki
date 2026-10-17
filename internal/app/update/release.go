package update

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	githubRepoPath = githubRepo + "/"

	releasesPerPage = 100
	maxReleasePages = 10
	// maxMetadataBytes bounds API response reads; a release listing is a few
	// hundred kilobytes at most.
	maxMetadataBytes = 4 << 20
)

// apiBase is overridden in tests to point at a stub server.
var apiBase = "https://api.github.com/"

// allowedRedirectHosts is the D4 pinned egress set: the GitHub API, release
// download pages, and their CDN asset hosts.
var allowedRedirectHosts = map[string]bool{
	"api.github.com":                       true,
	"github.com":                           true,
	"objects.githubusercontent.com":        true,
	"release-assets.githubusercontent.com": true,
}

// githubHTTPClient returns a client whose redirects are capped and pinned to
// GitHub hosts.
func githubHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return fmt.Errorf("redirect uses disallowed scheme %q", req.URL.Scheme)
			}
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			if !allowedRedirectHosts[req.URL.Host] {
				return fmt.Errorf("redirect to disallowed host %q", req.URL.Host)
			}

			// net/http can forward credentials to subdomains. Carry only the last
			// API request's authorization, never restoring it after an origin change.
			req.Header.Del("Authorization")
			previous := via[len(via)-1]
			if isGitHubAPIRequest(req) && isGitHubAPIRequest(previous) {
				if authorization := previous.Header.Get("Authorization"); authorization != "" {
					req.Header.Set("Authorization", authorization)
				}
			}
			return nil
		},
	}
}

func decodeBoundedJSON(r io.Reader, maxBytes int64, dest any) error {
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > maxBytes {
		return fmt.Errorf("JSON response exceeds %d bytes", maxBytes)
	}
	return json.Unmarshal(data, dest)
}

func isGitHubAPIRequest(req *http.Request) bool {
	return req.URL.Scheme == "https" && req.URL.Host == "api.github.com" && req.URL.User == nil
}

func newGitHubAPIRequest(ctx context.Context, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	if isGitHubAPIRequest(req) {
		token := os.Getenv("GH_TOKEN")
		if token == "" {
			token = os.Getenv("GITHUB_TOKEN")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	return req, nil
}

// getJSON performs a bounded GET and decodes exactly one JSON document.
func getJSON(ctx context.Context, url string, timeout time.Duration, dest any) error {
	req, err := newGitHubAPIRequest(ctx, url)
	if err != nil {
		return err
	}

	resp, err := githubHTTPClient(timeout).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API returned status %d for %s", resp.StatusCode, url)
	}
	if err := decodeBoundedJSON(resp.Body, maxMetadataBytes, dest); err != nil {
		return fmt.Errorf("decoding %s: %w", url, err)
	}
	return nil
}

type ghRelease struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

// fetchReleaseByTag resolves an exact published release. Drafts are invisible
// unauthenticated, so a 404 covers both missing and unpublished tags.
func fetchReleaseByTag(ctx context.Context, tag string) (ghRelease, error) {
	var release ghRelease
	url := apiBase + "repos/" + githubRepoPath + "releases/tags/" + tag
	if err := getJSON(ctx, url, 10*time.Second, &release); err != nil {
		return ghRelease{}, fmt.Errorf("release %s not found: %w", tag, err)
	}
	if release.Draft {
		return ghRelease{}, fmt.Errorf("release %s is a draft", tag)
	}
	if release.TagName != tag {
		return ghRelease{}, fmt.Errorf("release endpoint returned tag %q, expected %q", release.TagName, tag)
	}
	return release, nil
}

// listReleases scans a bounded number of pages and fails rather than silently
// choosing from a truncated release history.
func listReleases(ctx context.Context) ([]ghRelease, error) {
	var all []ghRelease
	for page := 1; page <= maxReleasePages; page++ {
		var releases []ghRelease
		url := apiBase + "repos/" + githubRepoPath + fmt.Sprintf("releases?per_page=%d&page=%d", releasesPerPage, page)
		if err := getJSON(ctx, url, 15*time.Second, &releases); err != nil {
			return nil, fmt.Errorf("listing releases: %w", err)
		}
		all = append(all, releases...)
		if len(releases) < releasesPerPage {
			return all, nil
		}
	}
	return nil, fmt.Errorf("release history exceeds scan limit of %d", releasesPerPage*maxReleasePages)
}

// fetchLatestRelease resolves the highest stable semantic version.
func fetchLatestRelease(ctx context.Context) (ghRelease, error) {
	releases, err := listReleases(ctx)
	if err != nil {
		return ghRelease{}, err
	}
	var newest ghRelease
	var newestVersion Version
	found := false
	for _, release := range releases {
		if release.Draft {
			continue
		}
		version, err := ParseVersion(release.TagName)
		if err != nil {
			return ghRelease{}, fmt.Errorf("published release tag rejected: %w", err)
		}
		if release.Prerelease || version.IsPrerelease() {
			continue
		}
		if !found || newestVersion.Compare(version) < 0 {
			found, newest, newestVersion = true, release, version
		}
	}
	if !found {
		return ghRelease{}, errors.New("no matching published release found")
	}
	return newest, nil
}

func resolveTagCommit(ctx context.Context, tag string) (string, error) {
	var object struct {
		Object struct {
			SHA  string `json:"sha"`
			Type string `json:"type"`
		} `json:"object"`
	}
	url := apiBase + "repos/" + githubRepoPath + "git/ref/tags/" + tag
	if err := getJSON(ctx, url, 10*time.Second, &object); err != nil {
		return "", fmt.Errorf("resolving tag %s commit: %w", tag, err)
	}
	for depth := 0; object.Object.Type == "tag" && depth < 5; depth++ {
		url = apiBase + "repos/" + githubRepoPath + "git/tags/" + object.Object.SHA
		if err := getJSON(ctx, url, 10*time.Second, &object); err != nil {
			return "", fmt.Errorf("peeling tag %s: %w", tag, err)
		}
	}
	if object.Object.Type != "commit" || !validCommit(object.Object.SHA) {
		return "", fmt.Errorf("tag %s does not resolve to a valid commit", tag)
	}
	return object.Object.SHA, nil
}

func resolveHeadCommit(ctx context.Context) (string, error) {
	var ref struct {
		Object struct {
			SHA  string `json:"sha"`
			Type string `json:"type"`
		} `json:"object"`
	}
	url := apiBase + "repos/" + githubRepoPath + "git/ref/heads/main"
	if err := getJSON(ctx, url, 10*time.Second, &ref); err != nil {
		return "", err
	}
	if ref.Object.Type != "commit" || !validCommit(ref.Object.SHA) {
		return "", errors.New("main does not resolve to a valid commit")
	}
	return ref.Object.SHA, nil
}

func validCommit(commit string) bool {
	if len(commit) != 40 && len(commit) != 64 {
		return false
	}
	_, err := hex.DecodeString(commit)
	return err == nil
}

// ResolveTarget returns the exact release tag the updater must install (D4):
// the pinned stable --version tag when set, otherwise the newest stable release.
// Unparsable tags fail closed instead of being silently skipped.
func ResolveTarget(ctx context.Context, opts Options) (string, error) {
	if opts.TargetVersion != "" {
		version, err := ParseVersion(opts.TargetVersion)
		if err != nil {
			return "", err
		}
		if version.IsPrerelease() {
			return "", fmt.Errorf("release %s is a prerelease; select a stable release or use --head for main", opts.TargetVersion)
		}
		release, err := fetchReleaseByTag(ctx, opts.TargetVersion)
		if err != nil {
			return "", err
		}
		if release.Prerelease {
			return "", fmt.Errorf("release %s is marked as a prerelease; select a stable release or use --head for main", release.TagName)
		}
		return release.TagName, nil
	}

	release, err := fetchLatestRelease(ctx)
	if err != nil {
		return "", err
	}
	if _, err := ParseVersion(release.TagName); err != nil {
		return "", fmt.Errorf("latest release tag rejected: %w", err)
	}
	return release.TagName, nil
}
