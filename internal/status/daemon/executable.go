package daemon

import (
	"errors"
	"os"
)

// executableImage compares the running binary with the file at its path, so
// that the daemon can stop after an update and let the next client start the
// new build.
type executableImage struct {
	path    string
	running os.FileInfo
}

func currentExecutable() (*executableImage, error) {
	path, err := os.Executable()
	if err != nil {
		return nil, err
	}

	running, err := statRunningExecutable(path)
	if err != nil {
		return nil, err
	}

	return &executableImage{path: path, running: running}, nil
}

// A removed path counts as replaced: the installation no longer provides this build.
func (e *executableImage) replaced() (bool, error) {
	current, err := os.Stat(e.path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}

	return !os.SameFile(e.running, current), nil
}
