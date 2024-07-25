package provider

import (
	"io"
	"os"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

type (
	// atlasHCL is the builder for the atlas.hcl file.
	atlasHCL struct {
		URL     string
		DevURL  string
		Source  string
		Schemas []string
		Exclude []string

		Diff      *Diff
		Migration *migrationConfig

		Cloud *cloudConfig
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
)

// we will allow the user configure the base atlas.hcl file
const baseAtlasHCL = "env {\n  name = atlas.env\n}"

// CreateFile writes the template data to
// atlas.hcl file in the given directory.
func (d *atlasHCL) CreateFile(name string) error {
	w, err := os.Create(name)
	if err != nil {
		return err
	}
	defer w.Close()
	return d.Write(w)
}

// Write writes the atlas config to the given writer.
func (d *atlasHCL) Write(w io.Writer) error {
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
	if d.Source != "" {
		e.SetAttributeValue("src", cty.StringVal(d.Source))
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
	if dd := d.Diff; dd != nil {
		d := e.AppendNewBlock("diff", nil).Body()
		if v := dd.ConcurrentIndex; v != nil {
			b := d.AppendNewBlock("concurrent_index", nil).Body()
			attrBoolPtr(b, v.Create, "create")
			attrBoolPtr(b, v.Drop, "drop")
		}
		if v := dd.Skip; v != nil {
			b := d.AppendNewBlock("skip", nil).Body()
			attrBoolPtr(b, v.AddSchema, "add_schema")
			attrBoolPtr(b, v.DropSchema, "drop_schema")
			attrBoolPtr(b, v.ModifySchema, "modify_schema")
			attrBoolPtr(b, v.AddTable, "add_table")
			attrBoolPtr(b, v.DropTable, "drop_table")
			attrBoolPtr(b, v.ModifyTable, "modify_table")
			attrBoolPtr(b, v.AddColumn, "add_column")
			attrBoolPtr(b, v.DropColumn, "drop_column")
			attrBoolPtr(b, v.ModifyColumn, "modify_column")
			attrBoolPtr(b, v.AddIndex, "add_index")
			attrBoolPtr(b, v.DropIndex, "drop_index")
			attrBoolPtr(b, v.ModifyIndex, "modify_index")
			attrBoolPtr(b, v.AddForeignKey, "add_foreign_key")
			attrBoolPtr(b, v.DropForeignKey, "drop_foreign_key")
			attrBoolPtr(b, v.ModifyForeignKey, "modify_foreign_key")
		}
	}
	dst, err := parseConfig(baseAtlasHCL)
	if err != nil {
		return err
	}
	mergeFile(dst, f)
	_, err = dst.WriteTo(w)
	return err
}

func attrBoolPtr(b *hclwrite.Body, v *bool, n string) {
	if v != nil {
		b.SetAttributeValue(n, cty.BoolVal(*v))
	}
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
