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
	yaml "github.com/zclconf/go-cty-yaml"
	"github.com/zclconf/go-cty/cty"
)

type (
	// AtlasSchemaDataSource defines the data source implementation.
	AtlasSchemaDataSource struct{}
	// AtlasSchemaDataSourceModel describes the data source data model.
	AtlasSchemaDataSourceModel struct {
		DevURL    types.String `tfsdk:"dev_db_url"`
		Src       types.String `tfsdk:"src"`
		HCL       types.String `tfsdk:"hcl"`
		ID        types.String `tfsdk:"id"`
		Variables types.List   `tfsdk:"variables"`
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
			"variables": {
				Description: "The variables used in the HCL",
				Optional:    true,
				Type: types.ListType{
					ElemType: types.StringType,
				},
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

	src := data.Src.Value
	if src == "" {
		// We don't have a schema to normalize,
		// so we don't do anything.
		data.ID = types.String{Value: hclID(nil)}
		data.HCL = types.String{Null: true}
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
		return
	}
	cli, err := sqlclient.Open(ctx, data.DevURL.Value)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to open connection, got error: %s", err))
		return
	}
	p := hclparse.NewParser()
	if _, err := p.ParseHCL([]byte(src), ""); err != nil {
		resp.Diagnostics.AddError("Parse HCL Error", fmt.Sprintf("Unable to parse HCL, got error: %s", err))
		return
	}
	var variables map[string]cty.Value
	if !data.Variables.IsNull() {
		variables = make(map[string]cty.Value)
		resp.Diagnostics.Append(ParseVariablesToHCL(ctx, data.Variables, variables)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	realm := &schema.Realm{}
	if err = cli.Evaluator.Eval(p, realm, variables); err != nil {
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
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func hclID(hcl []byte) string {
	h := fnv.New128()
	h.Write(hcl)
	return base64.RawStdEncoding.EncodeToString(h.Sum(nil))
}

func ParseVariablesToHCL(ctx context.Context, data types.List, variables map[string]cty.Value) (diags diag.Diagnostics) {
	var vars []string
	diags = data.ElementsAs(ctx, &vars, false)
	if diags.HasError() {
		return
	}
	for _, v := range vars {
		val, err := yaml.Unmarshal([]byte(v), cty.Map(cty.DynamicPseudoType))
		if err != nil {
			diags.AddError("Unmarshal Error",
				fmt.Sprintf("Unable to unmarshal, got error: %s, %s", err, v),
			)
			return
		}
		for k, v := range val.AsValueMap() {
			variables[k] = v
		}
	}
	return
}
