package provider

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"iter"
	"maps"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	"ariga.io/atlas/sql/migrate"
	"ariga.io/atlas/sql/sqlclient"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zclconf/go-cty/cty"

	atlas "ariga.io/atlas-go-sdk/atlasexec"
)

type (
	// projectConfig is the builder for the atlas.hcl file.
	projectConfig struct {
		Cloud *CloudConfig
		Env   *envConfig

		Config      string      // The base atlas.hcl to merge with, provided by the user
		Vars        atlas.Vars2 // Variable supplied for atlas.hcl
		EnvName     string      // The env name to report
		MigrateDown bool        // Allow TF run migrate down when detected
	}
	envConfig struct {
		URL       string
		DevURL    string
		Source    string
		Schemas   []string
		Exclude   []string
		Diff      *Diff
		Lint      *Lint
		Migration *migrationConfig
		Schema    *schemaConfig
	}
	CloudConfig struct {
		Token string
	}
	migrationConfig struct {
		DirURL          string
		Baseline        string
		ExecOrder       string
		RevisionsSchema string
		Repo            string
	}
	schemaConfig struct {
		Repo string
	}
)

// Render writes the atlas config to the given writer.
func (c *projectConfig) Render(w io.Writer) error {
	file, err := c.AsFile()
	if err != nil {
		return err
	}
	_, err = file.WriteTo(w)
	return err
}

// AsFile returns the merged atlas.hcl file as a *hclwrite.File.
func (c *projectConfig) AsFile() (*hclwrite.File, error) {
	dst, diags := hclwrite.ParseConfig([]byte(c.Config), "atlas.hcl", hcl.InitialPos)
	if diags.HasErrors() {
		return nil, diags
	}
	if err := mergeEnvBlock(dst.Body(), c.Env.AsBlock(), c.EnvName); err != nil {
		return nil, fmt.Errorf(`%w:

%s
`, err, c.Config)
	}
	return dst, nil
}

// LintReview returns the review attribute from the lint block in the env block.
func (c *projectConfig) LintReview() (*string, error) {
	file, err := c.AsFile()
	if err != nil {
		return nil, err
	}
	envBlk, err := searchBlock(file.Body(), "env", c.EnvName)
	if err != nil || envBlk == nil {
		return nil, err
	}
	lintBlk, err := searchBlock(envBlk.Body(), "lint", "")
	if err != nil || lintBlk == nil {
		return nil, err
	}
	if reviewAttr := lintBlk.Body().GetAttribute("review"); reviewAttr != nil {
		review := string(reviewAttr.Expr().BuildTokens(nil).Bytes())
		trimmed := strings.Trim(strings.TrimSpace(review), `"`)
		return &trimmed, nil
	}
	return nil, nil
}

// TargetURL returns the target URL for the environment.
func (c *projectConfig) TargetURL() (string, error) {
	file, err := c.AsFile()
	if err != nil {
		return "", err
	}
	envBlk, err := searchBlock(file.Body(), "env", c.EnvName)
	if err != nil || envBlk == nil {
		return "", err
	}
	if envBlk.Body().GetAttribute("src") != nil {
		return "env://src", nil
	}
	schemaBlk, err := searchBlock(envBlk.Body(), "schema", "")
	if err != nil || schemaBlk == nil {
		return "", err
	}
	if schemaBlk.Body().GetAttribute("src") != nil {
		return "env://schema.src", nil
	}
	return "", nil
}

// RepoURL returns the repository URL for the environment.
func (c *projectConfig) RepoURL() (*url.URL, error) {
	switch {
	// The user has provided the repository name in migration block.
	case c.Env.Migration != nil && c.Env.Migration.Repo != "":
		return &url.URL{
			Scheme: SchemaTypeAtlas,
			Host:   c.Env.Migration.Repo,
		}, nil
	// The user has provided the repository name in schema block.
	case c.Env.Schema != nil && c.Env.Schema.Repo != "":
		return &url.URL{
			Scheme: SchemaTypeAtlas,
			Host:   c.Env.Schema.Repo,
		}, nil
	// Fallback to desired URL if it's Cloud URL.
	case c.Env.URL != "":
		u, err := url.Parse(c.Env.URL)
		if err != nil {
			return nil, err
		}
		if u.Scheme != SchemaTypeAtlas {
			return nil, nil
		}
		c := *u
		c.RawQuery = ""
		return &c, nil
	// Search Repo URL in the project config.
	default:
		file, err := c.AsFile()
		if err != nil {
			return nil, err
		}
		envBlk, err := searchBlock(file.Body(), "env", c.EnvName)
		if err != nil || envBlk == nil {
			return nil, err
		}
		schemaBlk, err := searchBlock(envBlk.Body(), "schema", "")
		if err != nil || schemaBlk == nil {
			return nil, err
		}
		if schemaBlk.Body().GetAttribute("repo") != nil {
			// Build Repo URL from env.
			return &url.URL{
				Scheme: "env",
				Host:   "schema.repo",
			}, nil
		}
		return nil, nil
	}
}

// AsBlock returns the HCL block for the environment configuration.
func (env *envConfig) AsBlock() *hclwrite.Block {
	blk := hclwrite.NewBlock("env", nil)
	e := blk.Body()
	if env.URL != "" {
		e.SetAttributeValue("url", cty.StringVal(env.URL))
	}
	if env.DevURL != "" {
		e.SetAttributeValue("dev", cty.StringVal(env.DevURL))
	}
	if env.Source != "" {
		// src, schema attributes/blocks are mutually exclusive
		if sc := env.Schema; sc != nil && sc.Repo != "" {
			schema := e.AppendNewBlock("schema", nil).Body()
			schema.SetAttributeValue("src", cty.StringVal(env.Source))
			repo := schema.AppendNewBlock("repo", nil).Body()
			repo.SetAttributeValue("name", cty.StringVal(sc.Repo))
		} else {
			e.SetAttributeValue("src", cty.StringVal(env.Source))
		}
	}
	if l := deleteZero(env.Schemas); len(l) > 0 {
		e.SetAttributeValue("schemas", listStringVal(l))
	}
	if l := deleteZero(env.Exclude); len(l) > 0 {
		e.SetAttributeValue("exclude", listStringVal(l))
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
		if md.Repo != "" {
			repo := m.AppendNewBlock("repo", nil).Body()
			repo.SetAttributeValue("name", cty.StringVal(md.Repo))
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
	if ld := env.Lint; ld != nil && !ld.Review.IsNull() {
		l := e.AppendNewBlock("lint", nil).Body()
		l.SetAttributeValue("review", cty.StringVal(ld.Review.ValueString()))
	}
	return blk
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

func attrBoolPtr(b *hclwrite.Body, v types.Bool, n string) {
	if v.IsUnknown() || v.IsNull() {
		return
	}
	b.SetAttributeValue(n, cty.BoolVal(v.ValueBool()))
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

func mergeEnvBlock(dst *hclwrite.Body, blk *hclwrite.Block, name string) error {
	env, err := searchBlock(dst, "env", name)
	switch {
	case err != nil:
		return err
	case env == nil:
		// No env blocks found, create a new one.
		mergeBlock(dst.AppendNewBlock("env", []string{name}), blk)
		return nil
	default:
		// Found the block to merge with.
		mergeBlock(env, blk)
		return nil
	}
}

func searchBlock(parent *hclwrite.Body, typ, name string) (*hclwrite.Block, error) {
	blocks := parent.Blocks()
	typBlocks := make([]*hclwrite.Block, 0, len(blocks))
	for _, b := range blocks {
		if b.Type() == typ {
			typBlocks = append(typBlocks, b)
		}
	}
	if len(typBlocks) == 0 {
		// No things here, return nil.
		return nil, nil
	}
	// Check if there is a block with the given name.
	idx := slices.IndexFunc(typBlocks, func(b *hclwrite.Block) bool {
		labels := b.Labels()
		return len(labels) == 1 && labels[0] == name
	})
	if idx == -1 {
		// No block matched, check if there is an unnamed env block.
		idx = slices.IndexFunc(typBlocks, func(b *hclwrite.Block) bool {
			return len(b.Labels()) == 0
		})
		if idx == -1 {
			// Has blocks but none matched.
			return nil, fmt.Errorf(`the %s block %q was not found in the give config`, typ, name)
		}
	}
	return typBlocks[idx], nil
}

func mergeBlock(dst, src *hclwrite.Block) {
	dstBody, srcBody := dst.Body(), src.Body()
	for name, attr := range mapsSorted(srcBody.Attributes()) {
		dstBody.SetAttributeRaw(name, attr.Expr().BuildTokens(nil))
	}
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

// mapsSorted return a sequence of key-value pairs sorted by key.
func mapsSorted[K cmp.Ordered, V any](m map[K]V) iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for _, k := range slices.Sorted(maps.Keys(m)) {
			if !yield(k, m[k]) {
				return
			}
		}
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
	switch u, err := sqlclient.ParseURL(filepath.ToSlash(s)); {
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
