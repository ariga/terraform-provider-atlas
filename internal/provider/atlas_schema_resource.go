package provider

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"ariga.io/atlas/sql/schema"
	"ariga.io/atlas/sql/sqlclient"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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
		ID      types.String `tfsdk:"id"`
		HCL     types.String `tfsdk:"hcl"`
		URL     types.String `tfsdk:"url"`
		DevURL  types.String `tfsdk:"dev_db_url"`
		Exclude types.List   `tfsdk:"exclude"`
	}
)

// Ensure provider defined types fully satisfy framework interfaces
var (
	_ resource.Resource                   = &AtlasSchemaResource{}
	_ resource.ResourceWithModifyPlan     = &AtlasSchemaResource{}
	_ resource.ResourceWithValidateConfig = &AtlasSchemaResource{}
)

func (m AtlasSchemaResourceModel) Clone() *AtlasSchemaResourceModel {
	return &m
}

// NewAtlasSchemaResource returns a new AtlasSchemaResource.
func NewAtlasSchemaResource() resource.Resource {
	return &AtlasSchemaResource{}
}

// Metadata implements resource.Resource.
func (r *AtlasSchemaResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_schema"
}

// GetSchema implements resource.Resource.
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
				Validators: []tfsdk.AttributeValidator{
					stringvalidator.LengthAtLeast(1),
				},
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
			"exclude": {
				Description: "Filter out resources matching the given glob pattern. See https://atlasgo.io/declarative/inspect#exclude-schemas",
				Type: types.ListType{
					ElemType: types.StringType,
				},
				Optional: true,
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

// Create implements resource.Resource.
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

	// Only set ID when creating a new resource
	data.ID = types.String{Value: urlToID(data.URL.Value)}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Read implements resource.Resource.
func (r *AtlasSchemaResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *AtlasSchemaResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	realm, cli, diags := atlasInspect(ctx, data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	hcl, err := cli.MarshalSpec(realm)
	if err != nil {
		cli.Close()
		resp.Diagnostics.AddError("Marshal Error",
			fmt.Sprintf("Unable to marshal, got error: %s", err),
		)
		return
	}

	url := data.URL.Value
	data.ID = types.String{Value: urlToID(url)}
	data.URL = types.String{Value: url}
	data.HCL = types.String{Value: string(hcl)}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update implements resource.Resource.
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

// Delete implements resource.Resource.
func (r *AtlasSchemaResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *AtlasSchemaResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Delete the resource by setting
	// the HCL to an empty string
	data.HCL = types.String{Null: true}
	resp.Diagnostics.Append(r.applySchema(ctx, data)...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Validate implements resource.ResourceWithValidateConfig.
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

// ModifyPlan implements resource.ResourceWithModifyPlan.
func (r *AtlasSchemaResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	var plan *AtlasSchemaResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var state *AtlasSchemaResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if state == nil || state.HCL.Value == "" {
		if plan.URL.IsUnknown() {
			resp.RequiresReplace = append(resp.RequiresReplace, path.Root("url"))
			return
		}
		// New terraform resource will be create,
		// do the first run check to ensure the user doesn't
		// drops schema resources by accident
		resp.Diagnostics.Append(r.firstRunCheck(ctx, plan)...)
	}
	if plan == nil {
		// This is a delete operation
		if state == nil {
			// This is a delete operation on a resource that doesn't exist
			// in the state, so we can safely ignore it
			return
		}
		plan = state.Clone()
		// Delete the resource by setting
		// the HCL to an empty string.
		plan.HCL = types.String{Null: true}
	}
	resp.Diagnostics.Append(r.printPlanSQL(ctx, plan)...)
}

func (r *AtlasSchemaResource) printPlanSQL(ctx context.Context, data *AtlasSchemaResourceModel) (diags diag.Diagnostics) {
	createDesired := func(ctx context.Context, cli *sqlclient.Client) (desired *schema.Realm, err error) {
		desired = &schema.Realm{}
		if data.HCL.Value == "" {
			return
		}
		p := hclparse.NewParser()
		if _, err := p.ParseHCL([]byte(data.HCL.Value), ""); err != nil {
			return nil, err
		}
		if err = cli.Evaluator.Eval(p, desired, nil); err != nil {
			return
		}
		if data.DevURL.Value != "" {
			dev, err := sqlclient.Open(ctx, data.DevURL.Value)
			if err != nil {
				return nil, err
			}
			defer dev.Close()
			desired, err = dev.Driver.(schema.Normalizer).NormalizeRealm(ctx, desired)
			if err != nil {
				return nil, err
			}
		}
		return
	}
	changes, cli, diags := atlasChanges(ctx, data, createDesired)
	if diags.HasError() {
		return
	}
	if len(changes) > 0 {
		plan, err := cli.PlanChanges(ctx, "", changes)
		if err != nil {
			diags.AddError("Plan Error",
				fmt.Sprintf("Unable to plan changes, got error: %s", err),
			)
			return
		}
		buf := &strings.Builder{}
		for _, stmt := range plan.Changes {
			if stmt.Comment == "" {
				fmt.Fprintln(buf, stmt.Cmd)
			} else {
				fmt.Fprintf(buf, "-- %s\n%s\n", stmt.Comment, stmt.Cmd)
			}
		}
		diags.AddWarning("Atlas plan",
			fmt.Sprintf("The following SQL statements will be executed:\n\n\n%s", buf.String()),
		)
	}
	return diags
}

func (r *AtlasSchemaResource) applySchema(ctx context.Context, data *AtlasSchemaResourceModel) (diags diag.Diagnostics) {
	createDesired := func(ctx context.Context, cli *sqlclient.Client) (desired *schema.Realm, err error) {
		desired = &schema.Realm{}
		if data.HCL.Value == "" {
			return
		}
		p := hclparse.NewParser()
		if _, err := p.ParseHCL([]byte(data.HCL.Value), ""); err != nil {
			return nil, err
		}
		if err = cli.Evaluator.Eval(p, desired, nil); err != nil {
			return
		}
		if data.DevURL.Value != "" {
			dev, err := sqlclient.Open(ctx, data.DevURL.Value)
			if err != nil {
				return nil, err
			}
			defer dev.Close()
			desired, err = dev.Driver.(schema.Normalizer).NormalizeRealm(ctx, desired)
			if err != nil {
				return nil, err
			}
		}
		return
	}
	changes, cli, diags := atlasChanges(ctx, data, createDesired)
	if diags.HasError() {
		return
	}
	defer cli.Close()
	if err := cli.ApplyChanges(ctx, changes); err != nil {
		diags.AddError("Apply Error", fmt.Sprintf("Unable to apply changes, got error: %s", err))
		return
	}
	return diags
}

func (r *AtlasSchemaResource) firstRunCheck(ctx context.Context, data *AtlasSchemaResourceModel) (diags diag.Diagnostics) {
	createDesired := func(ctx context.Context, cli *sqlclient.Client) (desired *schema.Realm, err error) {
		desired = &schema.Realm{}
		p := hclparse.NewParser()
		if _, err := p.ParseHCL([]byte(data.HCL.Value), ""); err != nil {
			return nil, err
		}
		if err = cli.Evaluator.Eval(p, desired, nil); err != nil {
			return
		}
		return
	}
	changes, cli, diags := atlasChanges(ctx, data, createDesired)
	if diags.HasError() {
		return
	}
	defer cli.Close()

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

func atlasInspect(ctx context.Context, data *AtlasSchemaResourceModel) (_ *schema.Realm, cli *sqlclient.Client, diags diag.Diagnostics) {
	var exclude []string
	diags = data.Exclude.ElementsAs(ctx, &exclude, false)
	if diags.HasError() {
		return
	}
	nonEmptyExclude := make([]string, 0, len(exclude))
	for _, e := range exclude {
		if e != "" {
			nonEmptyExclude = append(nonEmptyExclude, e)
		}
	}
	cli, err := sqlclient.Open(ctx, data.URL.Value)
	if err != nil {
		diags.AddError(
			"Client Connection Error",
			fmt.Sprintf("Unable to open connection, got error: %s", err),
		)
		return
	}
	var schemas []string
	if cli.URL.Schema != "" {
		schemas = append(schemas, cli.URL.Schema)
	}
	realm, err := cli.InspectRealm(ctx, &schema.InspectRealmOption{
		Exclude: nonEmptyExclude,
		Schemas: schemas,
	})
	if err != nil {
		diags.AddError(
			"Inspect Error",
			fmt.Sprintf("Unable to inspect realm, got error: %s", err),
		)
		cli.Close()
		return
	}
	return realm, cli, diags
}

func atlasChanges(ctx context.Context, data *AtlasSchemaResourceModel, createDesired func(ctx context.Context, cli *sqlclient.Client) (*schema.Realm, error)) (changes []schema.Change, cli *sqlclient.Client, diags diag.Diagnostics) {
	current, cli, diags := atlasInspect(ctx, data)
	if diags.HasError() {
		return
	}
	desired, err := createDesired(ctx, cli)
	if err != nil {
		diags.AddError(
			"Create Desired Realm Error",
			fmt.Sprintf("Unable to create desired realm, got error: %s", err),
		)
		cli.Close()
		return
	}
	changes, err = cli.RealmDiff(current, desired)
	if err != nil {
		diags.AddError(
			"Diff Error",
			fmt.Sprintf("Unable to diff changes, got error: %s", err),
		)
		cli.Close()
		return
	}
	return
}

func urlToID(u string) string {
	uu, err := url.Parse(u)
	if err != nil {
		return u
	}
	uu.User = nil
	return uu.String()
}
