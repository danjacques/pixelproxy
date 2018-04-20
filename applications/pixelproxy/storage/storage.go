package storage

import (
	"context"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/danjacques/gopushpixels/replay/streamfile"
	"github.com/danjacques/pixelproxy/util"
	"github.com/danjacques/pixelproxy/util/logging"

	"github.com/pkg/errors"
)

const fileDataExt = ".protostream"

// S manages filesystem storage.
//
// The filesystem consists of a Root directory. It is assumed that S owns
// that directory and all files underneath of it.
//
// S's StreamWriter uses a temporary directory to create incomplete files,
// then atomically commits them to the main S directory using a move
// operation.
type S struct {
	// Root is the root of the storage filesystem.
	Root string

	// WriterCompression is the compression scheme to use when writing files.
	WriterCompression streamfile.Compression
	// WriterCompressionLevel is the compression level to use when writing
	// new files, if WriterCompression supports it.
	//
	// <0 means that a default compresison level should be used.
	WriterCompressionLevel int

	tempDir         string
	fileDir         string
	defaultFilePath string
}

// Prepare initializes the filesystem. This includes:
func (st *S) Prepare(c context.Context) error {
	// Ensure that the Root directory exists and is configured for S use.
	if st.Root == "" {
		return errors.New("no Root specified")
	}

	// Construct directories.
	st.Root = filepath.Clean(st.Root)
	st.tempDir = filepath.Join(st.Root, "temporary")
	st.fileDir = filepath.Join(st.Root, "files")
	st.defaultFilePath = filepath.Join(st.fileDir, "default")

	if err := os.MkdirAll(st.Root, 0755); err != nil {
		return errors.Wrapf(err, "failed to create root directory %q", st.Root)
	}

	// Clean up any temporary files from past Storage instances.
	switch _, err := os.Stat(st.tempDir); {
	case os.IsNotExist(err):
		// Nothing to clean up.
	case err == nil:
		// Temporary directory exists; destroy it.
		logging.S(c).Debugf("Removing temporary directory %q...", st.tempDir)
		if err := os.RemoveAll(st.tempDir); err != nil {
			return errors.Wrapf(err, "failed to remove temporary directory %q", st.tempDir)
		}
	default:
		// Unexpected error.
		return errors.Wrapf(err, "failed to stat temporary directory %q", st.tempDir)
	}

	// Create a new, empty temporary directory.
	if err := os.MkdirAll(st.tempDir, 0755); err != nil {
		return errors.Wrapf(err, "failed to create temporary directory %q", st.tempDir)
	}

	// Create our files directory.
	if err := os.MkdirAll(st.fileDir, 0755); err != nil {
		return errors.Wrapf(err, "failed to create temporary directory %q", st.fileDir)
	}

	// Clear any files that are invalid.
	if err := st.deleteInvalidFiles(c); err != nil {
		return errors.Wrap(err, "failed to delete invalid files")
	}

	logging.S(c).Debugf("Storage is set up at %q!", st.Root)
	return nil
}

// SetDefault sets the default file name.
//
// The default may be set, even if name does not exist. If name is empty, the
// default file name will be cleared.
func (st *S) SetDefault(name string) error {
	if name == "" {
		// Empty name means that we will clear the default (delete the file).
		if err := os.Remove(st.defaultFilePath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	return util.CreateViaTempMove(st.defaultFilePath, st.tempDir, "default", func(w io.Writer) error {
		_, err := w.Write([]byte(name))
		return err
	})
}

// GetDefault returns the current default file name.
//
// If there is no default, an empty string will be returned with a nil error.
func (st *S) GetDefault() (string, error) {
	content, err := ioutil.ReadFile(st.defaultFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			err = nil
		}
		return "", err
	}
	return string(content), nil
}

// ListFiles lists the contents of Storage in no particular order
//
// At the moment, we don't bother with paging. If we ever feel the need to do
// that, we will change this to implement a page token value and have a separate
// CountFiles call.
func (st *S) ListFiles(c context.Context) ([]*File, error) {
	// List all ".metadata" files.
	var files []*File
	err := util.ForEachFile(st.fileDir, func(fi os.FileInfo) error {
		// Only directories can be stream files.
		if !fi.IsDir() {
			return nil
		}

		// Filter those without the correct extension.
		name := fi.Name()
		if ext := filepath.Ext(name); ext != fileDataExt {
			return nil
		}

		path := filepath.Join(st.fileDir, name)
		id := strings.TrimSuffix(fi.Name(), fileDataExt)

		file, err := loadFileFromPath(path, id)
		if err != nil {
			logging.S(c).Debugf("Ignoring invalid file %q: %s", path, err)
			return nil
		}

		files = append(files, file)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return files, nil
}

func (st *S) makeFileForName(name string) *File {
	name = sanitizeDisplayName(name)
	id := fileIDFromDisplayName(name)

	return &File{
		DisplayName: name,
		ID:          id,
		Path:        filepath.Join(st.fileDir, id+fileDataExt),
	}
}

// OpenWriter opens a StreamWriter for a file with the specified name.
//
// The StreamWriter will commit the file when the stream is closed.
func (st *S) OpenWriter(name string) (*streamfile.EventStreamWriter, error) {
	cfg := st.eventStreamConfig()
	f := st.makeFileForName(name)

	return cfg.MakeEventStreamWriter(f.Path, f.DisplayName)
}

// OpenReader opens a StreamReader for a file with the specified name.
//
// The StreamReader will commit the file when the stream is closed.
func (st *S) OpenReader(name string) (*streamfile.EventStreamReader, error) {
	f := st.makeFileForName(name)
	return streamfile.MakeEventStreamReader(f.Path)
}

// DeleteFile deletes the file with the specified name.
func (st *S) DeleteFile(name string) error {
	f := st.makeFileForName(name)
	return streamfile.Delete(f.Path)
}

// MergeFiles merges the event streams in srcs together into a single event
// stream called name.
func (st *S) MergeFiles(dest string, srcs []string) error {
	cfg := st.eventStreamConfig()

	destF := st.makeFileForName(dest)
	srcPaths := make([]string, len(srcs))
	for i, src := range srcs {
		f := st.makeFileForName(src)
		srcPaths[i] = f.Path
	}

	return cfg.Merge(destF.Path, destF.DisplayName, srcPaths...)
}

func (st *S) eventStreamConfig() *streamfile.EventStreamConfig {
	return &streamfile.EventStreamConfig{
		TempDir:                st.tempDir,
		WriterCompression:      st.WriterCompression,
		WriterCompressionLevel: st.WriterCompressionLevel,
	}
}

func (st *S) deleteInvalidFiles(c context.Context) error {
	err := util.ForEachFile(st.fileDir, func(fi os.FileInfo) error {
		path := filepath.Join(st.fileDir, fi.Name())

		// Rule out known files.
		switch {
		case path == st.defaultFilePath:
			return nil
		}

		if err := streamfile.Validate(path); err != nil {
			logging.S(c).Warnf("File %q is out of place or invalid.", path)
		}

		return nil
	})
	if err != nil {
		return errors.Wrapf(err, "scanning files in %q", st.fileDir)
	}
	return nil
}
