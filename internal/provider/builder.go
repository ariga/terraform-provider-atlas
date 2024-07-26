package provider

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	"ariga.io/atlas/sql/migrate"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"

	atlas "ariga.io/atlas-go-sdk/atlasexec"
)

type (
	// projectConfig is the builder for the atlas.hcl file.
	projectConfig struct {
		EnvName string
		Cloud   *cloudConfig
		Env     *envConfig
		Config  string
	}
	envConfig struct {
		URL       string
		DevURL    string
		Source    string
		Schemas   []string
		Exclude   []string
		Diff      *Diff
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
)

// we will allow the user configure the base atlas.hcl file
const baseAtlasHCL = "env {\n}"

// Render writes the atlas config to the given writer.
func (c *projectConfig) Render(w io.Writer) error {
	dst, diags := hclwrite.ParseConfig([]byte(c.Config), "atlas.hcl", hcl.InitialPos)
	if diags.HasErrors() {
		return diags
	}
	mergeFile(dst, c.File())
	_, err := dst.WriteTo(w)
	return err
}

// File returns the HCL file representation of the project config.
func (c *projectConfig) File() *hclwrite.File {
	f := hclwrite.NewEmptyFile()
	r := f.Body()
	if cloud := c.Cloud; cloud != nil {
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
	if env := c.Env; env != nil {
		e := r.AppendNewBlock("env", nil).Body()
		e.SetAttributeTraversal("name", hcl.Traversal{
			hcl.TraverseRoot{Name: "atlas"},
			hcl.TraverseAttr{Name: "env"},
		})
		if env.URL != "" {
			e.SetAttributeValue("url", cty.StringVal(env.URL))
		}
		if env.DevURL != "" {
			e.SetAttributeValue("dev", cty.StringVal(env.DevURL))
		}
		if env.Source != "" {
			e.SetAttributeValue("src", cty.StringVal(env.Source))
		}
		if len(env.Schemas) > 0 {
			e.SetAttributeValue("schemas", listStringVal(env.Schemas))
		}
		if len(env.Exclude) > 0 {
			e.SetAttributeValue("exclude", listStringVal(env.Exclude))
		}
		if md := env.Migration; md != nil {
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
		if dd := env.Diff; dd != nil {
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
	}
	return f
}

// DirURL returns the URL to the migration directory.
func (c *envConfig) DirURL(wd *atlas.WorkingDir, ver string) (string, error) {
	if c.Migration == nil {
		return "", errors.New("missing migration directory in the config")
	}
	dirURL := c.Migration.DirURL
	switch u, err := url.Parse(dirURL); {
	case err != nil:
		return "", err
	case u.Scheme == SchemaTypeAtlas:
		// No need to create a new directory if the migration directory is remote.
		return dirURL, nil
	default:
		d, err := migrate.NewLocalDir(filepath.Join(u.Host, u.Path))
		if err != nil {
			return "", err
		}
		// in case of specifying a 'version' < latest, we need a new dir
		// that contains only the migrations up to the 'version'
		// helps getting the status of the migrations later
		cdir, err := NewChunkedDir(d, ver)
		if err != nil {
			return "", err
		}
		name := fmt.Sprintf("migration-%s", ver)
		if err = wd.CopyFS(name, cdir); err != nil {
			return "", err
		}
		return (&url.URL{
			Scheme: SchemaTypeFile,
			Path:   wd.Path(name),
		}).String(), nil
	}
}

// DirURLLatest returns the URL to the latest version of the migration directory.
// For example, atlas://remote-dir?tag=tag will return atlas://remote-dir.
// For local directories, it will return the same URL.
func (c *envConfig) DirURLLatest() (string, error) {
	if c.Migration == nil {
		return "", errors.New("missing migration directory in the config")
	}
	dirURL := c.Migration.DirURL
	switch u, err := url.Parse(dirURL); {
	case err != nil:
		return "", err
	case u.Scheme != SchemaTypeAtlas:
		return dirURL, nil
	default:
		// Remove the tag query parameter from the URL.
		// So, it will return the latest version of the directory.
		q := u.Query()
		q.Del("tag")
		u.RawQuery = q.Encode()
		return u.String(), nil
	}
}

func attrBoolPtr(b *hclwrite.Body, v *bool, n string) {
	if v != nil {
		b.SetAttributeValue(n, cty.BoolVal(*v))
	}
}

func listStringVal(s []string) cty.Value {
	v := make([]cty.Value, len(s))
	for i, s := range s {
		v[i] = cty.StringVal(s)
	}
	return cty.ListVal(v)
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

func parseConfig(cfg string) (*hclwrite.File, error) {
	f, diags := hclwrite.ParseConfig([]byte(cfg), "atlas.hcl", hcl.InitialPos)
	if diags.HasErrors() {
		return nil, diags
	}
	return f, nil
}

func mergeFile(dst, src *hclwrite.File) {
	dstBody, srcBody := dst.Body(), src.Body()
	dstBlocks := make(map[string]*hclwrite.Block)
	for _, blk := range dstBody.Blocks() {
		dstBlocks[address(blk)] = blk
	}
	for _, blk := range srcBody.Blocks() {
		if dstBlk, ok := dstBlocks[address(blk)]; ok {
			// Merge the blocks if they have the same address.
			mergeBlock(dstBlk, blk)
		} else {
			appendBlock(dstBody, blk)
		}
	}
}

func address(block *hclwrite.Block) string {
	parts := append([]string{block.Type()}, block.Labels()...)
	return strings.Join(parts, ".")
}

func mergeBlock(dst, src *hclwrite.Block) {
	dstBody, srcBody := dst.Body(), src.Body()
	safeLoop(srcBody.Attributes(), func(name string, attr *hclwrite.Attribute) {
		dstBody.SetAttributeRaw(name, attr.Expr().BuildTokens(nil))
	})
	srcBlocks := srcBody.Blocks()
	srcBlockTypes := make(map[string]struct{})
	for _, blk := range srcBlocks {
		srcBlockTypes[blk.Type()] = struct{}{}
	}
	for _, blk := range dstBody.Blocks() {
		if _, conflict := srcBlockTypes[blk.Type()]; conflict {
			// Remove the block from the destination if it already exists.
			dstBody.RemoveBlock(blk)
		}
	}
	for _, blk := range srcBlocks {
		appendBlock(dstBody, blk)
	}
}

// appendBlock appends a block to the body and ensures there is a newline before the block.
// It returns the appended block.
//
// There is a bug in hclwrite that causes the block to be appended without a newline
// https://github.com/hashicorp/hcl/issues/687
func appendBlock(body *hclwrite.Body, blk *hclwrite.Block) *hclwrite.Block {
	t := body.BuildTokens(nil)
	if len(t) == 0 || t[len(t)-1].Type != hclsyntax.TokenNewline {
		body.AppendNewline()
	}
	return body.AppendBlock(blk)
}

// safeLoop iterates over a map in a sorted order by key.
// Because looping over a map is not deterministic.
func safeLoop[K cmp.Ordered, V any](m map[K]V, fn func(K, V)) {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		fn(k, m[k])
	}
}

// absoluteFileURL returns the absolute path of a file URL.
func absoluteFileURL(s string) (string, error) {
	switch u, err := url.Parse(filepath.ToSlash(s)); {
	case err != nil:
		return "", fmt.Errorf("failed to parse migration directory URL: %w", err)
	case strings.ToLower(u.Scheme) == SchemaTypeAtlas:
		// Skip the URL if it is an atlas URL.
		return u.String(), nil
	default:
		// Convert relative path to absolute path
		absPath, err := filepath.Abs(filepath.Join(u.Host, u.Path))
		if err != nil {
			return "", fmt.Errorf("failed to get absolute path: %w", err)
		}
		return (&url.URL{
			Scheme:   SchemaTypeFile,
			Path:     absPath,
			RawQuery: u.RawQuery,
		}).String(), nil
	}
}

// absoluteSqliteURL returns the absolute path of a sqlite URL.
func absoluteSqliteURL(s string) (string, error) {
	if s == "" {
		// No URL to parse.
		return "", nil
	}
	switch u, err := url.Parse(filepath.ToSlash(s)); {
	case err != nil:
		return "", err
	case SchemaTypeSQLite != u.Scheme:
		return u.String(), nil
	default:
		path, err := filepath.Abs(filepath.Join(u.Host, u.Path))
		if err != nil {
			return "", err
		}
		return (&url.URL{
			Scheme:   u.Scheme,
			Path:     path,
			RawQuery: u.RawQuery,
		}).String(), nil
	}
}
