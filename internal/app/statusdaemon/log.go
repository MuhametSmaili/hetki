package statusdaemon

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	maxLogBytes      = 256 << 10
	maxLogErrorBytes = 1024
	logRepeatDelay   = time.Minute
)

type daemonLog struct {
	writer *logFile
	logger *log.Logger
	mu     sync.Mutex
	last   map[string]time.Time
}

// LogPath is available without a running daemon. One rotated file uses .1.
func (c *Client) LogPath() string {
	return strings.TrimSuffix(c.path, ".sock") + ".log"
}

func openDaemonLog(path string) (*daemonLog, error) {
	writer := &logFile{path: path}
	if err := writer.open(); err != nil {
		return nil, err
	}

	return &daemonLog{
		writer: writer,
		logger: log.New(writer, fmt.Sprintf("UTC pid=%d ", os.Getpid()), log.Ldate|log.Ltime|log.LUTC),
		last:   make(map[string]time.Time),
	}, nil
}

func (l *daemonLog) close() error {
	if l.writer.file == nil {
		return nil
	}

	return l.writer.file.Close()
}

func (l *daemonLog) info(event string) {
	l.logger.Printf("level=INFO event=%q", event)
}

func (l *daemonLog) ready(epoch string) {
	l.logger.Printf("level=INFO event=%q epoch=%s", "ready", epoch)
}

func (l *daemonLog) failure(event string, err error) {
	l.logger.Printf("level=ERROR event=%q error=%q", event, logMessage(err.Error()))
}

// Event names are fixed call-site constants, never request content.
func (l *daemonLog) repeatedFailure(event string, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	if previous := l.last[event]; !previous.IsZero() && now.Sub(previous) < logRepeatDelay {
		return
	}
	l.last[event] = now
	l.failure(event, err)
}

func (l *daemonLog) recordPanic() {
	value := recover()
	if value == nil {
		return
	}

	var stack [2048]byte
	n := runtime.Stack(stack[:], false)
	l.logger.Printf("level=ERROR event=%q value=%q stack=%q", "panic", panicMessage(value), stack[:n])
	panic(value)
}

func panicMessage(value any) string {
	switch value := value.(type) {
	case string:
		return logMessage(value)
	case error:
		return logMessage(value.Error())
	default:
		return fmt.Sprintf("panic value of type %T", value)
	}
}

func logMessage(message string) string {
	if len(message) > maxLogErrorBytes {
		return message[:maxLogErrorBytes] + " (truncated)"
	}

	return message
}
