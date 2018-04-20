package web

import (
	"time"
)

// FileList is a list of the current set of files.
type FileList struct {
	DefaultFileName string  `json:"default_file_name,omitempty"`
	Files           []*File `json:"files,omitempty"`
}

// File is a stored PixelPusher data file.
type File struct {
	Name              string        `json:"name"`
	DiskBytes         int64         `json:"diskBytes"`
	NumBytes          int64         `json:"numBytes"`
	NumEvents         int64         `json:"numEvents"`
	NumDevices        int           `json:"num_devices"`
	MaxStrips         int           `json:"max_strips"`
	MaxPixelsPerStrip int           `json:"max_pixels_per_strip"`
	Created           time.Time     `json:"created"`
	Duration          time.Duration `json:"duration"`
	Compression       string        `json:"compression"`
	IsDefault         bool          `json:"is_default"`
}
