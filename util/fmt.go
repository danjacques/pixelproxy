package util

import (
	"encoding/hex"
	"strings"
)

// Hex is a byte slice that encodes as a hex-dumped string.
//
// It can be used for easy lazy hex dumping.
type Hex []byte

func (h Hex) String() string { return hex.Dump([]byte(h)) }

// StringSlice lazily renders to a Delim-delimited string.
type StringSlice struct {
	S     []string
	Delim string
}

func (ss *StringSlice) String() string { return strings.Join(ss.S, ss.Delim) }
