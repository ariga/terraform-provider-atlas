package provider

import (
	_ "embed"
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

		RevisionsSchema string
	}
)

var (
	//go:embed config/migrate.tmpl
	cfgMigrate     string
	cfgMigrateTmpl = template.Must(template.New("migrate").
			Parse(cfgMigrate))
)

// CreateFile writes the template data to
// atlas.hcl file in the given directory.
func (d *templateData) CreateFile(name string) error {
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	defer f.Close()
	return cfgMigrateTmpl.Execute(f, d)
}
