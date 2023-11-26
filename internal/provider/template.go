package provider

import (
	"embed"
	"os"
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
)

var (
	//go:embed templates/*.tmpl
	tmpls embed.FS
	tmpl  = template.Must(template.New("terraform").
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
