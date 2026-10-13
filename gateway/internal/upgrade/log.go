package upgrade

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	DefaultLogFile = "/var/log/yaginx/upgrade.log"
	logMaxBytes    = 10 << 20
	logBackups     = 5
)

// operationLogger keeps operator-visible upgrade diagnostics in the invoking
// terminal (or systemd journal) and in the local rolling log file. Logging
// must never turn a recoverable upgrade into a failed one.
type operationLogger struct {
	mu    sync.Mutex
	out   io.Writer
	file  *os.File
	now   func() time.Time
	runID string
}

func newOperationLogger(out io.Writer, path string, now func() time.Time) *operationLogger {
	logger := &operationLogger{out: out, now: now}
	if logger.now == nil {
		logger.now = time.Now
	}
	logger.runID = fmt.Sprintf("%d", logger.now().UTC().UnixNano())
	if strings.TrimSpace(path) == "" {
		return logger
	}
	file, err := openRollingLog(path)
	if err != nil {
		logger.writeWarning("upgrade log file unavailable: " + err.Error())
		return logger
	}
	logger.file = file
	return logger
}

func (l *operationLogger) Close() {
	if l == nil || l.file == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.file.Close()
	l.file = nil
}

func (l *operationLogger) Event(stage, format string, args ...any) {
	if l == nil {
		return
	}
	l.write(fmt.Sprintf("stage=%s run_id=%s %s", stage, l.runID, fmt.Sprintf(format, args...)))
}

func (l *operationLogger) Output(stage string) io.WriteCloser {
	return &logLineWriter{logger: l, stage: stage}
}

func (l *operationLogger) writeWarning(message string) {
	l.write(fmt.Sprintf("stage=log.warning run_id=%s %s", l.runID, message))
}

func (l *operationLogger) write(message string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	line := fmt.Sprintf("%s %s\n", l.now().UTC().Format(time.RFC3339Nano), strings.TrimRight(message, "\n"))
	if l.out != nil {
		_, _ = io.WriteString(l.out, line)
	}
	if l.file != nil {
		if info, err := l.file.Stat(); err == nil && info.Size()+int64(len(line)) > logMaxBytes {
			path := l.file.Name()
			_ = l.file.Close()
			l.file = nil
			if err := rotateLog(path); err == nil {
				l.file, err = openLogFile(path)
			}
			if err != nil && l.out != nil {
				_, _ = fmt.Fprintf(l.out, "%s stage=log.warning run_id=%s upgrade log rotation failed: %v\n", l.now().UTC().Format(time.RFC3339Nano), l.runID, err)
			}
		}
	}
	if l.file != nil {
		_, _ = io.WriteString(l.file, line)
	}
}

type logLineWriter struct {
	logger *operationLogger
	stage  string
	mu     sync.Mutex
	buffer strings.Builder
}

func (w *logLineWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, line := range strings.SplitAfter(string(data), "\n") {
		w.buffer.WriteString(line)
		if strings.HasSuffix(line, "\n") {
			w.flush()
		}
	}
	return len(data), nil
}

func (w *logLineWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flush()
	return nil
}

func (w *logLineWriter) flush() {
	if w.buffer.Len() == 0 {
		return
	}
	line := strings.TrimSpace(w.buffer.String())
	w.buffer.Reset()
	if line != "" {
		w.logger.Event(w.stage, "%s", line)
	}
}

func openRollingLog(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("refuse symlink log file %s", path)
		}
		if info.Size() >= logMaxBytes {
			if err := rotateLog(path); err != nil {
				return nil, err
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return openLogFile(path)
}

func openLogFile(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func rotateLog(path string) error {
	for index := logBackups; index >= 1; index-- {
		from := fmt.Sprintf("%s.%d", path, index)
		if index == logBackups {
			if err := os.Remove(from); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
		to := fmt.Sprintf("%s.%d", path, index+1)
		if err := os.Rename(from, to); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return os.Rename(path, path+".1")
}
