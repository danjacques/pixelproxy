package util

import (
	"io"
	"os"

	"github.com/pkg/errors"
)

// ForEachFile iterates over every file in the specified directory path,
// invoking fn for each identified file.
//
// If path is not a directory, ForEachFile will return an error.
//
// If fn returns an error, iteration will stop and ForEachFile will return
// that error.
func ForEachFile(path string, fn func(os.FileInfo) error) error {
	const scanSize = 1024

	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		_ = dir.Close()
	}()

	// Assert that path is a directory.
	switch st, err := dir.Stat(); {
	case err != nil:
		return err
	case !st.IsDir():
		return errors.New("not a directory")
	}

	eof := false
	for !eof {
		files, err := dir.Readdir(scanSize)
		if err != nil {
			if err != io.EOF {
				return err
			}
			eof = true
		}

		// Invoke callback for each file.
		for _, f := range files {
			if err := fn(f); err != nil {
				return err
			}
		}
	}

	return nil
}
