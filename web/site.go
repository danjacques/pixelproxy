package web

import (
	"html/template"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// Site is a basic web site instance.
type Site struct {
	// Logger, if not nil, is the logger to use during Site operations.
	Logger *zap.Logger

	// Cache, if true, instructs the Site to cache any data or templates that it
	// loads.
	Cache bool

	// Roots is the set of path roots, used to load templates and other content.
	Roots map[string]AssetLoader

	// TemplateFuncMap is a map of functions that get injected into each template.
	TemplateFuncMap template.FuncMap

	templates map[string]*templateBuilder
}

// AddTemplate adds a template registration to s.
func (s *Site) AddTemplate(name string, dependencies ...string) error {
	tb := &templateBuilder{
		s:            s,
		name:         name,
		dependencies: dependencies,
	}

	if s.templates == nil {
		s.templates = make(map[string]*templateBuilder)
	}
	s.templates[name] = tb

	if s.Cache {
		// Seed the template.
		_, err := tb.getTemplate()
		if err != nil {
			return err
		}
	}

	return nil
}

// Render renders the specified asset to w.
func (s *Site) Render(w io.Writer, name string) error {
	data, err := s.getContent(name)
	if err != nil {
		return err
	}

	if _, err := w.Write(data); err != nil {
		return err
	}

	return nil
}

// RenderTemplate renders the specified template to w.
//
// RenderTemplate is not safe to call concurrently with AddTemplate; however,
// it is safe to call concurrently otherwise.
func (s *Site) RenderTemplate(w io.Writer, name string, data interface{}) error {
	tb := s.templates[name]
	if tb == nil {
		return &StatusError{
			Code: http.StatusNotFound,
			Err:  errors.Errorf("no template named %q", name),
		}
	}

	t, err := tb.getTemplate()
	if err != nil {
		return err
	}
	return t.ExecuteTemplate(w, name, data)
}

// getContent loads the specified content.
func (s *Site) getContent(name string) ([]byte, error) {
	// Split "root/...".
	parts := strings.SplitN(filepath.ToSlash(name), "/", 2)
	root := parts[0]
	if len(parts) > 1 {
		name = parts[1]
	} else {
		name = ""
	}

	// Get our asset loader.
	al := s.Roots[root]
	if al == nil {
		return nil, errors.Errorf("no root defined for %q", root)
	}
	return al.Load(name)
}
