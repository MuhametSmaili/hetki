package statusdaemon

import (
	"fmt"
	"os"
	"syscall"
)

// The endpoint lock gives one process ownership; log.Logger serializes writes.
// Close runs only after daemon workers have joined.
type logFile struct {
	path string
	file *os.File
	size int64
}

func (w *logFile) open() error {
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return err
	}

	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ownedMode(info, 0600) || !ok || stat.Nlink != 1 {
		_ = file.Close()
		return fmt.Errorf("unsafe status daemon log")
	}

	w.file, w.size = file, info.Size()
	return nil
}

func (w *logFile) Write(data []byte) (int, error) {
	if len(data) > maxLogBytes {
		return 0, fmt.Errorf("status daemon log record exceeds size limit")
	}

	if w.file == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}

	if w.size+int64(len(data)) > maxLogBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}

	n, err := w.file.Write(data)
	w.size += int64(n)
	return n, err
}

func (w *logFile) rotate() error {
	err := w.file.Close()
	w.file = nil
	if err != nil {
		return err
	}

	if err := os.Rename(w.path, w.path+".1"); err != nil {
		return err
	}

	return w.open()
}
