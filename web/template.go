package web

import (
	"html/template"
	"sync"

	"github.com/pkg/errors"
)

type templateBuilder struct {
	s            *Site
	name         string
	dependencies []string

	calcOnce sync.Once
	t        *template.Template
	err      error
}

func (tb *templateBuilder) getTemplate() (*template.Template, error) {
	// If we're caching, calculate at most once.
	if tb.s.Cache {
		// Build the template once.
		tb.calcOnce.Do(func() {
			tb.t, tb.err = tb.buildTemplate()
		})
		return tb.t, tb.err
	}

	return tb.buildTemplate()
}

func (tb *templateBuilder) buildTemplate() (*template.Template, error) {
	// Build a new template tree.
	t := template.New("").Funcs(tb.s.TemplateFuncMap)

	augment := func(name string) (*template.Template, error) {
		content, err := tb.s.getContent(name)
		if err != nil {
			return nil, errors.Errorf("failed to get content for %q", name)
		}

		t, err := t.New(name).Parse(string(content))
		if err != nil {
			return nil, errors.Wrapf(err, "could not parse template %q", name)
		}

		return t, nil
	}

	for _, dep := range tb.dependencies {
		var err error
		if t, err = augment(dep); err != nil {
			return nil, err
		}
	}

	return augment(tb.name)
}
