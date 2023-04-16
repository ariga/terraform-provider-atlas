package provider

import (
	"context"
	"encoding/base64"
	"fmt"
	"hash/fnv"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"ariga.io/ariga/terraform-provider-atlas/internal/atlas"
)

type (
	// AtlasSchemaDataSource defines the data source implementation.
	AtlasSchemaDataSource struct {
		client *atlas.Client
	}
	// AtlasSchemaDataSourceModel describes the data source data model.
	AtlasSchemaDataSourceModel struct {
		DevURL types.String `tfsdk:"dev_db_url"`
		Src    types.String `tfsdk:"src"`
		HCL    types.String `tfsdk:"hcl"`
		ID     types.String `tfsdk:"id"`
	}
)

// Ensure provider defined types fully satisfy framework interfaces
var (
	_ datasource.DataSource              = &AtlasSchemaDataSource{}
	_ datasource.DataSourceWithConfigure = &AtlasSchemaDataSource{}
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
func (d *AtlasSchemaDataSource) GetSchema(ctx context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		// This description is used by the documentation generator and the language server.
		Description: "atlas_schema data source uses dev-db to normalize the HCL schema " +
			"in order to create better terraform diffs",
		Attributes: map[string]tfsdk.Attribute{
			"dev_db_url": {
				Description: "The url of the dev-db see https://atlasgo.io/cli/url",
				Type:        types.StringType,
				Required:    true,
				Sensitive:   true,
			},
			"src": {
				Description: "The schema definition of the database",
				Type:        types.StringType,
				Required:    true,
			},
			// the HCL in a predicted, and ordered format see https://atlasgo.io/cli/dev-database
			"hcl": {
				Description: "The normalized form of the HCL",
				Type:        types.StringType,
				Computed:    true,
			},
			"id": {
				Description: "The ID of this resource",
				Type:        types.StringType,
				Computed:    true,
			},
		},
	}, nil
}

// Configure implements datasource.DataSourceWithConfigure.
func (d *AtlasSchemaDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*atlas.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *atlas.MigrateClient, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}
	d.client = c
}

// Read implements datasource.DataSource.
func (d *AtlasSchemaDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data AtlasSchemaDataSourceModel

	// Read Terraform configuration data into the model
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	src := data.Src.Value
	if src == "" {
		// We don't have a schema to normalize,
		// so we don't do anything.
		data.ID = types.String{Value: hclID(nil)}
		data.HCL = types.String{Null: true}
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
	normalHCL, err := d.client.SchemaInspect(ctx, &atlas.SchemaInspectParams{
		DevURL: data.DevURL.Value,
		Format: "hcl",
		URL:    src,
	})
	if err != nil {
		resp.Diagnostics.AddError("Inspect Error",
			fmt.Sprintf("Unable to inspect given source, got error: %s", err),
		)
		return
	}
	data.ID = types.String{Value: hclID([]byte(normalHCL))}
	data.HCL = types.String{Value: normalHCL}
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
