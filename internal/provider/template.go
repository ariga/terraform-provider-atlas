package provider

import (
	"embed"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/template"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

type (
	// atlasHCL is the builder for the atlas.hcl file.
	atlasHCL struct {
		URL     string
		DevURL  string
		Schemas []string
		Exclude []string

		Cloud     *cloudConfig
		Migration *migrationConfig
	}
	cloudConfig struct {
		Token   string
		Project *string
		URL     *string
	}
	migrationConfig struct {
		DirURL          string
		Baseline        string
		ExecOrder       string
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
			"hclValue": hclValue,
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
func (d *atlasHCL) CreateFile(baseConfig, name string) error {
	f := hclwrite.NewEmptyFile()
	r := f.Body()
	if cloud := d.Cloud; cloud != nil {
		a := r.AppendNewBlock("atlas", nil).Body()
		c := a.AppendNewBlock("cloud", nil).Body()
		c.SetAttributeValue("token", cty.StringVal(cloud.Token))
		if cloud.Project != nil {
			c.SetAttributeValue("project", cty.StringVal(*cloud.Project))
		}
		if cloud.URL != nil {
			c.SetAttributeValue("url", cty.StringVal(*cloud.URL))
		}
	}
	e := r.AppendNewBlock("env", nil).Body()
	if d.URL != "" {
		e.SetAttributeValue("url", cty.StringVal(d.URL))
	}
	if d.DevURL != "" {
		e.SetAttributeValue("dev", cty.StringVal(d.DevURL))
	}
	if len(d.Schemas) > 0 {
		s := make([]cty.Value, len(d.Schemas))
		for i, v := range d.Schemas {
			s[i] = cty.StringVal(v)
		}
		e.SetAttributeValue("schemas", cty.ListVal(s))
	}
	if len(d.Exclude) > 0 {
		s := make([]cty.Value, len(d.Exclude))
		for i, v := range d.Exclude {
			s[i] = cty.StringVal(v)
		}
		e.SetAttributeValue("exclude", cty.ListVal(s))
	}
	if md := d.Migration; md != nil {
		m := e.AppendNewBlock("migration", nil).Body()
		if md.DirURL != "" {
			m.SetAttributeValue("dir", cty.StringVal(md.DirURL))
		}
		if md.Baseline != "" {
			m.SetAttributeValue("baseline", cty.StringVal(md.Baseline))
		}
		if md.ExecOrder != "" {
			m.SetAttributeTraversal("exec_order", hcl.Traversal{
				hcl.TraverseRoot{Name: hclValue(md.ExecOrder)},
			})
		}
		if md.RevisionsSchema != "" {
			m.SetAttributeValue("revisions_schema", cty.StringVal(md.RevisionsSchema))
		}
	}
	dst, err := parseConfig(baseConfig)
	if err != nil {
		return err
	}
	mergeFile(dst, f)
	w, err := os.Create(name)
	if err != nil {
		return err
	}
	defer w.Close()
	_, err = dst.WriteTo(w)
	return err
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

// hclValue returns the given string in
// HCL format. For example, linear-skip becomes
// LINEAR_SKIP.
func hclValue(s string) string {
	if s == "" {
		return ""
	}
	return strings.ReplaceAll(strings.ToUpper(s), "-", "_")
}
