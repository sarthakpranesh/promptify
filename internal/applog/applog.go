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
func Setup() (logPath string, closer io.Closer, err error) {

	// build the log files path
	path := os.Getenv("PROMPTIFY_LOG_PATH")
	if path == "" {
		path = filepath.Join("data", "promptify.log")
	}

	// create the log files directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", nil, err
	}

	// check if the log file is too large and rename it if it is
	if st, err := os.Stat(path); err == nil && st.Size() >= maxBytes {
		_ = os.Rename(path, path+".1")
	}

	// open the log file
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", nil, err
	}

	// set the log output and flags
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	log.SetFlags(log.LstdFlags | log.LUTC)

	// return the log path and the closer
	return path, f, nil
}
