package util

import (
	"io"
	"io/ioutil"
	"os"
)

// CreateViaTempMove atomically writes a file containing content to path.
//
// In order to do this atomically, we first create the file in a temporary
// directory and, upon success, move it into place via an atomic move.
//
// On failure, cleanup is best-effort.
func CreateViaTempMove(path, tempDir, prefix string, fn func(w io.Writer) error) error {
	// Write the file somewhere.
	fd, err := ioutil.TempFile(tempDir, prefix)
	if err != nil {
		return err
	}
	tmpPath := fd.Name()

	// Cleanup on finish/failure. We'll empty fd/tmpPath as we progress.
	defer func() {
		if fd != nil {
			_ = fd.Close()
		}
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()

	// Generate file contents.
	if err := fn(fd); err != nil {
		return err
	}

	// Close the file.
	if err := fd.Close(); err != nil {
		return err
	}
	fd = nil // Don't close in defer.

	// Move it into place.
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	tmpPath = "" // Don't delete in defer.

	return nil
}
