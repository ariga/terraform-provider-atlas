package provider

import (
	"context"
	"fmt"
	"strings"

	"ariga.io/atlas/sql/schema"
	"ariga.io/atlas/sql/sqlclient"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type (
	// AtlasSchemaResource defines the resource implementation.
	AtlasSchemaResource struct{}
	// AtlasSchemaResourceModel describes the resource data model.
	AtlasSchemaResourceModel struct {
		ID     types.String `tfsdk:"id"`
		HCL    types.String `tfsdk:"hcl"`
		URL    types.String `tfsdk:"url"`
		DevURL types.String `tfsdk:"dev_db_url"`
	}
)

// Ensure provider defined types fully satisfy framework interfaces
var (
	_ resource.Resource                   = &AtlasSchemaResource{}
	_ resource.ResourceWithModifyPlan     = &AtlasSchemaResource{}
	_ resource.ResourceWithValidateConfig = &AtlasSchemaResource{}
)

func NewAtlasSchemaResource() resource.Resource {
	return &AtlasSchemaResource{}
}

func (r *AtlasSchemaResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_schema"
}

func (r *AtlasSchemaResource) GetSchema(ctx context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: "Atlas database resource manages the data schema of the database, " +
			"using an HCL file describing the wanted state of the database. " +
			"See https://atlasgo.io/",
		Attributes: map[string]tfsdk.Attribute{
			"hcl": {
				Description: "The schema definition for the database " +
					"(preferably normalized - see `atlas_schema` data source)",
				Type:     types.StringType,
				Required: true,
			},
			"url": {
				Description: "The url of the database see https://atlasgo.io/cli/url",
				Type:        types.StringType,
				Required:    true,
				Sensitive:   true,
			},
			"dev_db_url": {
				Description: "The url of the dev-db see https://atlasgo.io/cli/url",
				Type:        types.StringType,
				Optional:    true,
				Sensitive:   true,
			},
			"id": {
				Description: "The ID of this resource",
				Computed:    true,
				PlanModifiers: tfsdk.AttributePlanModifiers{
					resource.UseStateForUnknown(),
				},
				Type: types.StringType,
			},
		},
	}, nil
}

func (r *AtlasSchemaResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *AtlasSchemaResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(r.applySchema(ctx, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	data.ID = types.String{Value: data.URL.Value}
	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AtlasSchemaResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *AtlasSchemaResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var (
		diags = resp.Diagnostics
		url   = data.URL.Value
	)
	cli, err := sqlclient.Open(ctx, url)
	if err != nil {
		diags.AddError("Client Error", fmt.Sprintf("Unable to open connection, got error: %s", err))
		return
	}
	var schemas []string
	if cli.URL.Schema != "" {
		schemas = append(schemas, cli.URL.Schema)
	}
	realm, err := cli.InspectRealm(ctx, &schema.InspectRealmOption{Schemas: schemas})
	if err != nil {
		diags.AddError("Inspect Error", fmt.Sprintf("Unable to inspect realm, got error: %s", err))
		return
	}
	hcl, err := cli.MarshalSpec(realm)
	if err != nil {
		diags.AddError("Marshal Error", fmt.Sprintf("Unable to marshal, got error: %s", err))
		return
	}

	data.ID = types.String{Value: url}
	data.URL = types.String{Value: url}
	data.HCL = types.String{Value: string(hcl)}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AtlasSchemaResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *AtlasSchemaResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(r.applySchema(ctx, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AtlasSchemaResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *AtlasSchemaResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cli, err := sqlclient.Open(ctx, data.URL.Value)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to open connection, got error: %s", err))
		return
	}
	var schemas []string
	if cli.URL.Schema != "" {
		schemas = append(schemas, cli.URL.Schema)
	}
	realm, err := cli.InspectRealm(ctx, &schema.InspectRealmOption{
		Schemas: schemas,
	})
	if err != nil {
		resp.Diagnostics.AddError("Inspect Error", fmt.Sprintf("Unable to inspect realm, got error: %s", err))
		return
	}
	changes, err := cli.RealmDiff(realm, &schema.Realm{})
	if err != nil {
		resp.Diagnostics.AddError("Diff Error", fmt.Sprintf("Unable to diff changes, got error: %s", err))
		return
	}
	if err = cli.ApplyChanges(ctx, changes); err != nil {
		resp.Diagnostics.AddError("Apply Error", fmt.Sprintf("Unable to apply changes, got error: %s", err))
		return
	}
}

func (r AtlasSchemaResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data AtlasSchemaResourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if data.DevURL.Value == "" {
		resp.Diagnostics.AddAttributeWarning(
			path.Root("dev_db_url"),
			"dev_db_url is unset",
			"It is highly recommended that you use 'dev_db_url' to specify a dev database.\n"+
				"to learn more about it, visit: https://atlasgo.io/dev-database")
	}
}

func (r *AtlasSchemaResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	var state *AtlasSchemaResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if state != nil && state.HCL.Value != "" {
		// This isn't a new resource, so we don't need to do anything
		return
	}

	var plan *AtlasSchemaResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(r.firstRunCheck(ctx, plan)...)
}

func (r *AtlasSchemaResource) applySchema(ctx context.Context, data *AtlasSchemaResourceModel) (diags diag.Diagnostics) {
	var (
		hcl = data.HCL.Value
		url = data.URL.Value
	)
	cli, err := sqlclient.Open(ctx, url)
	if err != nil {
		diags.AddError("Client Error", fmt.Sprintf("Unable to open connection, got error: %s", err))
		return
	}
	var schemas []string
	if cli.URL.Schema != "" {
		schemas = append(schemas, cli.URL.Schema)
	}
	realm, err := cli.InspectRealm(ctx, &schema.InspectRealmOption{Schemas: schemas})
	if err != nil {
		diags.AddError("Inspect Error", fmt.Sprintf("Unable to inspect realm, got error: %s", err))
		return
	}
	p := hclparse.NewParser()
	if _, err := p.ParseHCL([]byte(hcl), ""); err != nil {
		diags.AddError("Parse HCL Error", fmt.Sprintf("Unable to parse HCL, got error: %s", err))
		return
	}
	desired := &schema.Realm{}
	if err = cli.Evaluator.Eval(p, desired, nil); err != nil {
		diags.AddError("Eval HCL Error", fmt.Sprintf("Unable to eval HCL, got error: %s", err))
		return
	}
	if data.DevURL.Value != "" {
		dev, err := sqlclient.Open(ctx, data.DevURL.Value)
		if err != nil {
			diags.AddError("Client Error", fmt.Sprintf("Unable to open connection, got error: %s", err))
			return
		}
		defer dev.Close()
		desired, err = dev.Driver.(schema.Normalizer).NormalizeRealm(ctx, desired)
		if err != nil {
			diags.AddError("Normalize Error", fmt.Sprintf("Unable to normalize, got error: %s", err))
			return
		}
	}
	changes, err := cli.RealmDiff(realm, desired)
	if err != nil {
		diags.AddError("Diff Error", fmt.Sprintf("Unable to diff changes, got error: %s", err))
		return
	}
	if err = cli.ApplyChanges(ctx, changes); err != nil {
		diags.AddError("Apply Error", fmt.Sprintf("Unable to apply changes, got error: %s", err))
		return
	}
	return diags
}

func (r *AtlasSchemaResource) firstRunCheck(ctx context.Context, data *AtlasSchemaResourceModel) (diags diag.Diagnostics) {
	var (
		hcl = data.HCL.Value
		url = data.URL.Value
	)
	cli, err := sqlclient.Open(ctx, url)
	if err != nil {
		diags.AddError("Client Error", fmt.Sprintf("Unable to open connection, got error: %s", err))
		return
	}
	var schemas []string
	if cli.URL.Schema != "" {
		schemas = append(schemas, cli.URL.Schema)
	}
	current, err := cli.InspectRealm(ctx, &schema.InspectRealmOption{Schemas: schemas})
	if err != nil {
		diags.AddError("Inspect Error", fmt.Sprintf("Unable to inspect realm, got error: %s", err))
		return
	}
	p := hclparse.NewParser()
	if _, err := p.ParseHCL([]byte(hcl), ""); err != nil {
		diags.AddError("Parse HCL Error", fmt.Sprintf("Unable to parse HCL, got error: %s", err))
		return
	}
	desired := &schema.Realm{}
	if err = cli.Evaluator.Eval(p, desired, nil); err != nil {
		diags.AddError("Eval HCL Error", fmt.Sprintf("Unable to eval HCL, got error: %s", err))
		return
	}
	changes, err := cli.RealmDiff(current, desired)
	if err != nil {
		diags.AddError("Diff Error", fmt.Sprintf("Unable to diff changes, got error: %s", err))
		return
	}
	var causes []string
	for _, c := range changes {
		switch c := c.(type) {
		case *schema.DropSchema:
			causes = append(causes, fmt.Sprintf("DROP SCHEMA %q", c.S.Name))
		case *schema.DropTable:
			causes = append(causes, fmt.Sprintf("DROP TABLE %q", c.T.Name))
		case *schema.ModifyTable:
			for _, c1 := range c.Changes {
				switch t := c1.(type) {
				case *schema.DropColumn:
					causes = append(causes, fmt.Sprintf("DROP COLUMN %q.%q", c.T.Name, t.C.Name))
				case *schema.DropIndex:
					causes = append(causes, fmt.Sprintf("DROP INDEX %q.%q", c.T.Name, t.I.Name))
				case *schema.DropForeignKey:
					causes = append(causes, fmt.Sprintf("DROP FOREIGN KEY %q.%q", c.T.Name, t.F.Symbol))
				case *schema.DropAttr:
					causes = append(causes, fmt.Sprintf("DROP ATTRIBUTE %q.%T", c.T.Name, t.A))
				case *schema.DropCheck:
					causes = append(causes, fmt.Sprintf("DROP CHECK CONSTRAINT %q.%q", c.T.Name, t.C.Name))
				}
			}
		}
	}
	if len(causes) > 0 {
		diags.AddError(
			"Unrecognized schema resources",
			fmt.Sprintf(`The database contains resources that Atlas wants to drop because they are not defined in the HCL file on the first run.
- %s
To learn how to add an existing database to a project, read:
https://atlasgo.io/terraform-provider#working-with-an-existing-database`, strings.Join(causes, "\n- ")))
	}

	return
}
