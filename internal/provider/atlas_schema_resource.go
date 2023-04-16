package provider

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"ariga.io/ariga/terraform-provider-atlas/internal/atlas"
)

type (
	// AtlasSchemaResource defines the resource implementation.
	AtlasSchemaResource struct {
		client *atlas.Client
	}
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
	_ resource.ResourceWithConfigure      = &AtlasSchemaResource{}
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

func (r *AtlasSchemaResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*atlas.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *atlas.MigrateClient, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}
	r.client = c
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
	var exclude []string
	resp.Diagnostics.Append(data.GetExclude(ctx, &exclude)...)
	if resp.Diagnostics.HasError() {
		return
	}
	hcl, err := r.client.SchemaInspect(ctx, &atlas.SchemaInspectParams{
		URL:     data.URL.Value,
		Exclude: exclude,
		Format:  "hcl",
	})
	if err != nil {
		resp.Diagnostics.AddError("Inspect Error",
			fmt.Sprintf("Unable to inspect, got error: %s", err),
		)
		return
	}
	data.HCL = types.String{Value: hcl}
	data.ID = types.String{Value: urlToID(data.URL.Value)}

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
	if !data.DevURL.IsUnknown() && data.DevURL.Value == "" {
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
		if plan == nil {
			return
		}
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
	resp.Diagnostics.Append(PrintPlanSQL(ctx, r.client, plan)...)
}

func PrintPlanSQL(ctx context.Context, c *atlas.Client, data *AtlasSchemaResourceModel) (diags diag.Diagnostics) {
	to, cleanup, err := data.handleHCL()
	if err != nil {
		diags.AddError("HCL Error",
			fmt.Sprintf("Unable to parse HCL, got error: %s", err),
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
	var exclude []string
	diags.Append(data.GetExclude(ctx, &exclude)...)
	if diags.HasError() {
		return
	}
	result, err := c.SchemaApply(ctx, &atlas.SchemaApplyParams{
		DevURL:  data.DevURL.Value,
		DryRun:  true,
		Exclude: exclude,
		To:      to,
		URL:     data.URL.Value,
	})
	if err != nil {
		diags.AddError("Atlas Plan Error",
			fmt.Sprintf("Unable to generate migration plan, got error: %s", err),
		)
		return
	}
	if len(result.Changes.Pending) > 0 {
		buf := &strings.Builder{}
		for _, stmt := range result.Changes.Pending {
			fmt.Fprintln(buf, stmt)
		}
		diags.AddWarning("Atlas Plan",
			fmt.Sprintf("The following SQL statements will be executed:\n\n\n%s", buf.String()),
		)
	}
	return diags
}

func (r *AtlasSchemaResource) applySchema(ctx context.Context, data *AtlasSchemaResourceModel) (diags diag.Diagnostics) {
	var exclude []string
	diags.Append(data.GetExclude(ctx, &exclude)...)
	if diags.HasError() {
		return
	}
	to, cleanup, err := data.handleHCL()
	if err != nil {
		diags.AddError("HCL Error",
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
	_, err = r.client.SchemaApply(ctx, &atlas.SchemaApplyParams{
		DevURL:  data.DevURL.Value,
		Exclude: exclude,
		To:      to,
		URL:     data.URL.Value,
	})
	if err != nil {
		diags.AddError("Apply Error",
			fmt.Sprintf("Unable to apply changes, got error: %s", err),
		)
		return
	}
	return diags
}

func (r *AtlasSchemaResource) firstRunCheck(ctx context.Context, data *AtlasSchemaResourceModel) (diags diag.Diagnostics) {
	to, cleanup, err := data.handleHCL()
	if err != nil {
		diags.AddError("HCL Error",
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
	var exclude []string
	diags.Append(data.GetExclude(ctx, &exclude)...)
	if diags.HasError() {
		return
	}
	result, err := r.client.SchemaApply(ctx, &atlas.SchemaApplyParams{
		DevURL:  data.DevURL.Value,
		DryRun:  true,
		Exclude: exclude,
		To:      to,
		URL:     data.URL.Value,
	})
	if err != nil {
		diags.AddError("Atlas Plan Error",
			fmt.Sprintf("Unable to generate migration plan, got error: %s", err),
		)
		return
	}
	var causes []string
	for _, c := range result.Changes.Pending {
		if strings.Contains(c, "DROP ") {
			causes = append(causes, c)
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

func nonEmptyStringSlice(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func urlToID(u string) string {
	uu, err := url.Parse(u)
	if err != nil {
		return u
	}
	uu.User = nil
	return uu.String()
}

func (data *AtlasSchemaResourceModel) handleHCL() (string, func() error, error) {
	return atlas.TempFile(data.HCL.Value, "hcl")
}

func (data *AtlasSchemaResourceModel) GetExclude(ctx context.Context, exclude *[]string) (diags diag.Diagnostics) {
	diags = data.Exclude.ElementsAs(ctx, exclude, false)
	if diags.HasError() {
		return
	}
	*exclude = nonEmptyStringSlice(*exclude)
	return
}
