//go:build !linux

package daemon

import "os"

// Without /proc/self/exe, a replacement between exec and this call goes unnoticed.
func statRunningExecutable(path string) (os.FileInfo, error) {
	return os.Stat(path)
}
