package provider

import (
	"context"
	"encoding/base64"
	"fmt"
	"hash/fnv"
	"net/url"

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
	if !isURL(src) {
		var (
			cleanup func() error
			err     error
		)
		src, cleanup, err = atlas.TempFile(src, "hcl")
		if err != nil {
			resp.Diagnostics.AddError("HCL Error",
				fmt.Sprintf("Unable to create temporary file for HCL, got error: %s", err),
			)
			return
		}
		defer func() {
			if err := cleanup(); err != nil {
				tflog.Debug(ctx, "Failed to remove HCL file", map[string]interface{}{
					"error": err,
				})
			}
		}()
	}
	var vars atlas.Vars
	if !data.Variables.IsNull() {
		vars = make(atlas.Vars)
		resp.Diagnostics.Append(data.Variables.ElementsAs(ctx, &vars, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	normalHCL, err := d.client.SchemaInspect(ctx, &atlas.SchemaInspectParams{
		DevURL: d.getDevURL(data.DevURL),
		URL:    src,
		Vars:   vars,
	})
	if err != nil {
		resp.Diagnostics.AddError("Inspect Error",
			fmt.Sprintf("Unable to inspect given source, got error: %s", err),
		)
		return
	}
	data.ID = types.StringValue(hclID([]byte(normalHCL)))
	data.HCL = types.StringValue(normalHCL)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func isURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme != ""
}

func hclID(hcl []byte) string {
	h := fnv.New128()
	h.Write(hcl)
	return base64.RawStdEncoding.EncodeToString(h.Sum(nil))
}
