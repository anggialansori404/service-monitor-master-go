package logutil

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DailyRotatingWriter writes to a log file named server-YYYY-MM-DD.log
// and automatically switches to a new file at midnight.
type DailyRotatingWriter struct {
	mu       sync.Mutex
	dir      string
	file     *os.File
	currentDay string
}

// NewDailyRotatingWriter creates a writer that writes to logs/server-YYYY-MM-DD.log.
// It creates the logs directory if needed.
func NewDailyRotatingWriter(dir string) (*DailyRotatingWriter, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}

	w := &DailyRotatingWriter{dir: dir}
	if err := w.rotate(); err != nil {
		return nil, err
	}

	return w, nil
}

// rotate opens a new log file for the current date.
func (w *DailyRotatingWriter) rotate() error {
	if w.file != nil {
		w.file.Close()
	}

	today := time.Now().Format("2006-01-02")
	filename := filepath.Join(w.dir, fmt.Sprintf("server-%s.log", today))

	f, err := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open log file %s: %w", filename, err)
	}

	w.file = f
	w.currentDay = today
	return nil
}

// Write implements io.Writer. It checks if the day has changed and rotates if needed.
func (w *DailyRotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	today := time.Now().Format("2006-01-02")
	if today != w.currentDay {
		if err := w.rotate(); err != nil {
			// Fallback: write to stderr so logs are not lost
			log.Printf("ERROR rotating log file: %v", err)
			return os.Stderr.Write(p)
		}
	}

	return w.file.Write(p)
}

// Close closes the underlying file.
func (w *DailyRotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

// Setup configures both the standard log package and returns a MultiWriter
// so logs go to both the rotating file AND stdout (for docker/k8s compatibility).
func Setup(logDir string) (*DailyRotatingWriter, error) {
	writer, err := NewDailyRotatingWriter(logDir)
	if err != nil {
		return nil, err
	}

	// Write to both file and stdout
	multiWriter := io.MultiWriter(writer, os.Stdout)

	log.SetOutput(multiWriter)
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	return writer, nil
}
