package provider

import (
	"context"
	"encoding/base64"
	"fmt"
	"hash/fnv"
	"net/url"
	"path/filepath"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	atlas "ariga.io/atlas-go-sdk/atlasexec"
)

type (
	// AtlasSchemaDataSource defines the data source implementation.
	AtlasSchemaDataSource struct {
		providerData
	}
	// AtlasSchemaDataSourceModel describes the data source data model.
	AtlasSchemaDataSourceModel struct {
		DevURL    types.String `tfsdk:"dev_url"`
		Src       types.String `tfsdk:"src"`
		HCL       types.String `tfsdk:"hcl"`
		ID        types.String `tfsdk:"id"`
		Variables types.Map    `tfsdk:"variables"`
	}
)

// Ensure provider defined types fully satisfy framework interfaces
var (
	_ datasource.DataSourceWithValidateConfig = &AtlasSchemaDataSource{}
	_ datasource.DataSource                   = &AtlasSchemaDataSource{}
	_ datasource.DataSourceWithConfigure      = &AtlasSchemaDataSource{}
)

// NewAtlasSchemaDataSource returns a new AtlasSchemaDataSource.
func NewAtlasSchemaDataSource() datasource.DataSource {
	return &AtlasSchemaDataSource{}
}

// Metadata implements datasource.DataSource.
func (d *AtlasSchemaDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_schema"
}

// GetSchema implements datasource.DataSource.
func (d *AtlasSchemaDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		Description: "atlas_schema data source uses dev-db to normalize the HCL schema " +
			"in order to create better terraform diffs",
		Attributes: map[string]schema.Attribute{
			"dev_url": schema.StringAttribute{
				Description: "The url of the dev-db see https://atlasgo.io/cli/url",
				Required:    true,
				Sensitive:   true,
			},
			"src": schema.StringAttribute{
				Description: "The schema definition of the database. This attribute can be HCL schema or an URL to HCL/SQL file.",
				Required:    true,
			},
			// the HCL in a predicted, and ordered format see https://atlasgo.io/cli/dev-database
			"hcl": schema.StringAttribute{
				Description: "The normalized form of the HCL",
				Computed:    true,
			},
			"id": schema.StringAttribute{
				Description: "The ID of this resource",
				Computed:    true,
			},
			"variables": schema.MapAttribute{
				Description: "The map of variables used in the HCL.",
				Optional:    true,
				ElementType: types.StringType,
			},
		},
	}
}

// Configure implements datasource.DataSourceWithConfigure.
func (d *AtlasSchemaDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	resp.Diagnostics.Append(d.configure(req.ProviderData)...)
}

// ValidateConfig implements datasource.DataSourceWithValidateConfig.
func (d *AtlasSchemaDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	resp.Diagnostics.Append(d.validateConfig(ctx, req.Config)...)
}

// Read implements datasource.DataSource.
func (d *AtlasSchemaDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data AtlasSchemaDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	src := data.Src.ValueString()
	if src == "" {
		// We don't have a schema to normalize,
		// so we don't do anything.
		data.ID = types.StringValue(hclID(nil))
		data.HCL = types.StringNull()
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
		return
	}
	var vars atlas.Vars
	if !data.Variables.IsNull() {
		vars = make(atlas.Vars)
		resp.Diagnostics.Append(data.Variables.ElementsAs(ctx, &vars, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	cfg, wd, err := data.projectConfig(d.devURL)
	if err != nil {
		resp.Diagnostics.AddError("HCL Error",
			fmt.Sprintf("Unable to create working directory, got error: %s", err),
		)
		return
	}
	defer func() {
		if err := wd.Close(); err != nil {
			tflog.Debug(ctx, "Failed to remove HCL file", map[string]interface{}{
				"error": err,
			})
		}
	}()
	c, err := d.client(wd.Path())
	if err != nil {
		resp.Diagnostics.AddError("Client Error",
			fmt.Sprintf("Unable to create client, got error: %s", err),
		)
		return
	}
	hcl, err := c.SchemaInspect(ctx, &atlas.SchemaInspectParams{
		Env:  cfg.EnvName,
		Vars: vars,
	})
	if err != nil {
		resp.Diagnostics.AddError("Inspect Error",
			fmt.Sprintf("Unable to inspect given source, got error: %s", err),
		)
		return
	}
	data.HCL = types.StringValue(hcl)
	data.ID = types.StringValue(hclID([]byte(data.HCL.ValueString())))
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (d *AtlasSchemaDataSourceModel) projectConfig(devURL string) (*projectConfig, *atlas.WorkingDir, error) {
	cfg := &projectConfig{
		Config:  baseAtlasHCL,
		EnvName: "tf",
		Env: &envConfig{
			URL:    "file://schema.hcl",
			DevURL: defaultString(d.DevURL, devURL),
		},
	}
	opts := []atlas.Option{atlas.WithAtlasHCL(cfg.Render)}
	u, err := url.Parse(filepath.ToSlash(d.Src.ValueString()))
	if err == nil && u.Scheme == SchemaTypeFile {
		// Convert relative path to absolute path
		absPath, err := filepath.Abs(filepath.Join(u.Host, u.Path))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get absolute path: %w", err)
		}
		cfg.Env.URL = (&url.URL{
			Scheme:   SchemaTypeFile,
			Path:     absPath,
			RawQuery: u.RawQuery,
		}).String()
	} else {
		opts = append(opts, func(wd *atlas.WorkingDir) error {
			_, err := wd.WriteFile("schema.hcl", []byte(d.Src.ValueString()))
			return err
		})
	}
	wd, err := atlas.NewWorkingDir(opts...)
	if err != nil {
		return nil, nil, err
	}
	return cfg, wd, nil
}

func hclID(hcl []byte) string {
	h := fnv.New128()
	h.Write(hcl)
	return base64.RawStdEncoding.EncodeToString(h.Sum(nil))
}
