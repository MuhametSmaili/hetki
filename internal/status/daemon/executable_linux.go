package daemon

import "os"

// /proc/self/exe names the running file even after its path is replaced.
func statRunningExecutable(string) (os.FileInfo, error) {
	return os.Stat("/proc/self/exe")
}
