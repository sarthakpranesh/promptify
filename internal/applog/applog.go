package applog

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

const maxBytes = 10 * 1024 * 1024 // 10MB

// Setup opens path for append, optionally rotates if oversized, and tees log to stderr + file.
// Returns log path and a closer.
func Setup(path string) (logPath string, closer io.Closer, err error) {
	if path == "" {
		path = filepath.Join("data", "promptify.log")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", nil, err
	}

	if st, err := os.Stat(path); err == nil && st.Size() >= maxBytes {
		_ = os.Rename(path, path+".1")
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", nil, err
	}

	log.SetOutput(io.MultiWriter(os.Stderr, f))
	log.SetFlags(log.LstdFlags | log.LUTC)
	return path, f, nil
}
