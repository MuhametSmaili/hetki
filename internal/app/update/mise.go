package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type miseInstall struct {
	Version          string `json:"version"`
	RequestedVersion string `json:"requested_version"`
	InstallPath      string `json:"install_path"`
	Active           bool   `json:"active"`
	Installed        bool   `json:"installed"`
}

func miseUpdater(ctx context.Context, exePath string, opts Options) (Updater, error) {
	installs, err := miseInstalls(ctx)
	if err != nil {
		return nil, err
	}
	install, err := matchMiseInstall(exePath, installs)
	if err != nil || install == nil {
		return nil, err
	}
	if !opts.Head {
		return nil, errors.New("mise-managed Hetki supports only --head; use mise for release updates")
	}
	if !install.Installed || !install.Active || install.Version != "main" || install.RequestedVersion != "main" {
		return nil, fmt.Errorf("--head requires mise's active main installation; select go:%s@main with mise first", modulePath)
	}
	// Linked installations may share a target with a pinned version or another tool.
	for _, path := range []string{install.InstallPath, filepath.Join(install.InstallPath, "bin"), filepath.Join(install.InstallPath, "bin", "hetki")} {
		if err := rejectSymlinkedInstall(path); err != nil {
			return nil, fmt.Errorf("mise-linked installations cannot self-update: %w", err)
		}
	}
	return &GoUpdater{exePath: exePath, Verbose: opts.Verbose}, nil
}

func miseInstalls(ctx context.Context) ([]miseInstall, error) {
	mise, err := exec.LookPath("mise")
	if errors.Is(err, exec.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out, err := commandOutput(ctx, 10*time.Second, 1<<20, mise, "ls", "--installed", "--json", "go:"+modulePath)
	if err != nil {
		return nil, err
	}
	var installs []miseInstall
	if err := json.Unmarshal(out, &installs); err != nil {
		return nil, fmt.Errorf("decoding mise installations: %w", err)
	}
	if installs == nil {
		return nil, errors.New("mise returned no installation metadata")
	}
	return installs, nil
}

func matchMiseInstall(exePath string, installs []miseInstall) (*miseInstall, error) {
	if len(installs) == 0 {
		return nil, nil
	}
	exeReal, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		return nil, err
	}
	var match *miseInstall
	for _, install := range installs {
		if !filepath.IsAbs(install.InstallPath) {
			return nil, errors.New("mise returned an invalid installation path")
		}
		candidate, err := filepath.EvalSymlinks(filepath.Join(install.InstallPath, "bin", "hetki"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if candidate != exeReal {
			continue
		}
		if match != nil {
			return nil, errors.New("multiple mise installations refer to this executable; update through mise")
		}
		match = &install
	}
	return match, nil
}
