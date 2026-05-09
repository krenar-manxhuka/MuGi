// Package prompts loads and renders prompt templates for each agent.
//
// Templates are resolved in order:
//  1. The directory pointed to by the PROMPTS_DIR environment variable
//     (defaults to "prompts" relative to the working directory).
//  2. The embedded templates compiled into the binary (internal/prompts/templates/).
//
// This lets users customise prompts by editing files in the root prompts/
// directory without touching compiled code.
package prompts

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
)

//go:embed templates/*.tmpl
var embedded embed.FS

// Loader resolves and renders prompt templates.
type Loader struct {
	dir string // optional on-disk override directory
}

// NewLoader returns a Loader that checks dir for overrides before falling back
// to the embedded templates. Pass an empty string to use embedded only.
func NewLoader(dir string) *Loader {
	return &Loader{dir: dir}
}

// Render loads the template named <name>.tmpl, executes it with data, and
// returns the rendered string.
func (l *Loader) Render(name string, data any) (string, error) {
	src, err := l.load(name)
	if err != nil {
		return "", err
	}

	tmpl, err := template.New(name).Parse(src)
	if err != nil {
		return "", fmt.Errorf("prompts: parse %q: %w", name, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("prompts: render %q: %w", name, err)
	}
	return buf.String(), nil
}

func (l *Loader) load(name string) (string, error) {
	if l.dir != "" {
		path := filepath.Join(l.dir, name+".tmpl")
		if data, err := os.ReadFile(path); err == nil {
			return string(data), nil
		}
	}
	data, err := embedded.ReadFile("templates/" + name + ".tmpl")
	if err != nil {
		return "", fmt.Errorf("prompts: template %q not found (checked %q and embedded)", name, l.dir)
	}
	return string(data), nil
}
