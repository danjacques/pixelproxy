package storage

import (
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/danjacques/gopushpixels/replay/streamfile"

	"github.com/pkg/errors"
)

// File is a single stored File.
type File struct {
	// DisplayName is the display name of this file.
	DisplayName string

	// ID is the internal ID of the File, a sanitized DisplayName.
	ID string

	// Path is the path to this File's data.
	Path string

	// Size is the size of this file on disk, in bytes.
	Size int64

	// Metadata is this File's metadata block.
	Metadata *streamfile.Metadata
}

func loadFileFromPath(path, id string) (*File, error) {
	md, size, err := streamfile.LoadMetadataAndSize(path)
	if err != nil {
		return nil, err
	}

	// Create a base file. We'll fill it in when we load its metadata.
	f := File{
		ID:          id,
		Path:        path,
		DisplayName: md.Name,
		Size:        size,
		Metadata:    md,
	}
	return &f, nil
}

// Delete deletes the File's storage presence.
//
// If the file does not exist, Delete is considered successful.
func (f *File) Delete() error {
	if err := os.Remove(f.Path); err != nil && !os.IsNotExist(err) {
		return errors.Wrapf(err, "removing path %q", f.Path)
	}
	return nil
}

// sanitizeDisplayName converts a dirty display name string into a canonical
// display name.
func sanitizeDisplayName(v string) string { return strings.TrimSpace(v) }

// fileIDForName replaces non-ASCII alphanumeric characters with escapes.
func fileIDFromDisplayName(v string) string {
	isValid := func(r rune) bool {
		if r > unicode.MaxASCII {
			return false
		}
		return unicode.IsLetter(r) || unicode.IsNumber(r)
	}
	must := func(v int, err error) {
		if err != nil {
			panic(err)
		}
	}

	var sb strings.Builder
	for _, r := range v {
		if isValid(r) {
			must(sb.WriteRune(r))
		} else {
			must(fmt.Fprintf(&sb, "_%x_", r))
		}
	}
	return sb.String()
}
