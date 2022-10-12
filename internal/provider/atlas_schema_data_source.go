package provider

import (
	"context"
	"encoding/base64"
	"fmt"
	"hash/fnv"

	"ariga.io/atlas/sql/schema"
	"ariga.io/atlas/sql/sqlclient"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type (
	// AtlasSchemaDataSource defines the data source implementation.
	AtlasSchemaDataSource struct{}
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
	_ datasource.DataSource = &AtlasSchemaDataSource{}
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

// Read implements datasource.DataSource.
func (d *AtlasSchemaDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data AtlasSchemaDataSourceModel

	// Read Terraform configuration data into the model
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var (
		src = data.Src.Value
		url = data.DevURL.Value
	)
	cli, err := sqlclient.Open(ctx, url)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to open connection, got error: %s", err))
		return
	}
	p := hclparse.NewParser()
	if _, err := p.ParseHCL([]byte(src), ""); err != nil {
		resp.Diagnostics.AddError("Parse HCL Error", fmt.Sprintf("Unable to parse HCL, got error: %s", err))
		return
	}
	realm := &schema.Realm{}
	if err = cli.Evaluator.Eval(p, realm, nil); err != nil {
		resp.Diagnostics.AddError("Eval HCL Error", fmt.Sprintf("Unable to eval HCL, got error: %s", err))
		return
	}
	realm, err = cli.Driver.(schema.Normalizer).NormalizeRealm(ctx, realm)
	if err != nil {
		resp.Diagnostics.AddError("Normalize Error", fmt.Sprintf("Unable to normalize, got error: %s", err))
		return
	}
	normalHCL, err := cli.MarshalSpec(realm)
	if err != nil {
		resp.Diagnostics.AddError("Marshal Error", fmt.Sprintf("Unable to marshal, got error: %s", err))
		return
	}

	data.ID = types.String{Value: hclID(normalHCL)}
	data.HCL = types.String{Value: string(normalHCL)}

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func hclID(hcl []byte) string {
	h := fnv.New128()
	h.Write(hcl)
	return base64.RawStdEncoding.EncodeToString(h.Sum(nil))
}
