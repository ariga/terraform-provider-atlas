package provider

import (
	"embed"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/template"
)

type (
	cloudConfig struct {
		Token   string
		Project *string
		URL     *string
	}
	remoteDir struct {
		Name string
		Tag  *string
	}
	templateData struct {
		URL     string
		DevURL  string
		Schemas []string
		Exclude []string

		Cloud     *cloudConfig
		DirURL    *string
		RemoteDir *remoteDir

		Baseline        string
		RevisionsSchema string
	}
	schemaData struct {
		Source  string
		URL     string
		DevURL  string
		Schemas []string
		Exclude []string
		Diff    *Diff
	}
)

var (
	//go:embed templates/*.tmpl
	tmpls embed.FS
	tmpl  = template.Must(template.New("terraform").
		Funcs(template.FuncMap{
			"hclValue": func(s string) string {
				if s == "" {
					return s
				}
				return strings.ReplaceAll(strings.ToUpper(s), "-", "_")
			},
			"slides": func(s []string) (string, error) {
				b := &strings.Builder{}
				b.WriteRune('[')
				for i, v := range s {
					if i > 0 {
						b.WriteRune(',')
					}
					fmt.Fprintf(b, "%q", v)
				}
				b.WriteRune(']')
				return b.String(), nil
			},
		}).
		ParseFS(tmpls, "templates/*.tmpl"),
	)
)

// CreateFile writes the template data to
// atlas.hcl file in the given directory.
func (d *templateData) CreateFile(name string) error {
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	defer f.Close()
	return tmpl.ExecuteTemplate(f, "atlas_migration.tmpl", d)
}

// render renders the atlas.hcl template.
//
// The template is used by the Atlas CLI to apply the schema.
// It also validates the data before rendering the template.
func (d *schemaData) render(w io.Writer) error {
	if d.URL == "" {
		return errors.New("database url is not set")
	}
	return tmpl.ExecuteTemplate(w, "atlas_schema.tmpl", d)
}
