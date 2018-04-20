package web

import (
	"os"

	"github.com/gobuffalo/packr"
	"github.com/pkg/errors"
)

// ErrNotFound is a sentinel error returned by an AssetLoader when the asset
// is not defined.
var ErrNotFound = errors.New("asset not found")

// AssetLoader is a generic interface to something that returns asset data
// by name.
type AssetLoader interface {
	// Load loads the specified asset. If the asset couldn't be found, Load
	// should return ErrNotFound.
	Load(name string) ([]byte, error)
}

// AssetLoaderChain is a chain of asset loaders. It iterates through each
// AssetLoader attempting to load the named asset. If one AssetLoader returns
// ErrNotFound, the next loader in the chain is consulted.
type AssetLoaderChain []AssetLoader

// Load implements AssetLoader.
func (alc AssetLoaderChain) Load(name string) ([]byte, error) {
	for _, al := range alc {
		switch data, err := al.Load(name); err {
		case nil:
			return data, nil
		case ErrNotFound:
			// Try the next AssetLoader in the chain.
		default:
			return nil, err
		}
	}
	return nil, ErrNotFound
}

// PackrBox is an AssetLoader that loads from a packr.Box.
type PackrBox struct {
	packr.Box
}

// Load implements AssetLoader.
func (pb *PackrBox) Load(name string) ([]byte, error) {
	// Load "name" from the Box, b.
	switch data, err := pb.MustBytes(name); {
	case err == nil:
		return data, nil
	case os.IsNotExist(err):
		return nil, ErrNotFound
	default:
		return nil, err
	}
}
