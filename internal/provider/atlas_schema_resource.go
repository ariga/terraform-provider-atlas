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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	atlas "ariga.io/atlas-go-sdk/atlasexec"
)

type (
	// AtlasSchemaResource defines the resource implementation.
	AtlasSchemaResource struct {
		providerData
	}
	// AtlasSchemaResourceModel describes the resource data model.
	AtlasSchemaResourceModel struct {
		ID      types.String `tfsdk:"id"`
		HCL     types.String `tfsdk:"hcl"`
		URL     types.String `tfsdk:"url"`
		DevURL  types.String `tfsdk:"dev_url"`
		Exclude types.List   `tfsdk:"exclude"`
		// Policies
		Diff *Diff `tfsdk:"diff"`

		DeprecatedDevURL types.String `tfsdk:"dev_db_url"`
	}
	// Diff defines the diff policies to apply when planning schema changes.
	Diff struct {
		ConcurrentIndex *ConcurrentIndex `tfsdk:"concurrent_index"`
		Skip            *SkipChanges     `tfsdk:"skip"`
	}
	ConcurrentIndex struct {
		Create *bool `tfsdk:"create"`
		Drop   *bool `tfsdk:"drop"`
	}
	// SkipChanges represents the skip changes policy.
	SkipChanges struct {
		AddSchema        *bool `tfsdk:"add_schema"`
		DropSchema       *bool `tfsdk:"drop_schema"`
		ModifySchema     *bool `tfsdk:"modify_schema"`
		AddTable         *bool `tfsdk:"add_table"`
		DropTable        *bool `tfsdk:"drop_table"`
		ModifyTable      *bool `tfsdk:"modify_table"`
		AddColumn        *bool `tfsdk:"add_column"`
		DropColumn       *bool `tfsdk:"drop_column"`
		ModifyColumn     *bool `tfsdk:"modify_column"`
		AddIndex         *bool `tfsdk:"add_index"`
		DropIndex        *bool `tfsdk:"drop_index"`
		ModifyIndex      *bool `tfsdk:"modify_index"`
		AddForeignKey    *bool `tfsdk:"add_foreign_key"`
		DropForeignKey   *bool `tfsdk:"drop_foreign_key"`
		ModifyForeignKey *bool `tfsdk:"modify_foreign_key"`
	}
)

// Ensure provider defined types fully satisfy framework interfaces
var (
	_ resource.Resource                   = &AtlasSchemaResource{}
	_ resource.ResourceWithModifyPlan     = &AtlasSchemaResource{}
	_ resource.ResourceWithConfigure      = &AtlasSchemaResource{}
	_ resource.ResourceWithValidateConfig = &AtlasSchemaResource{}
)

var (
	diffBlock = schema.SingleNestedBlock{
		Blocks: map[string]schema.Block{
			"concurrent_index": schema.SingleNestedBlock{
				Description: "The concurrent index policy",
				Attributes: map[string]schema.Attribute{
					"create": boolOptional("Whether to create indexes concurrently"),
					"drop":   boolOptional("Whether to drop indexes concurrently"),
				},
			},
			"skip": schema.SingleNestedBlock{
				Description: "The skip changes policy",
				Attributes: map[string]schema.Attribute{
					"add_schema":         boolOptional("Whether to skip adding schemas"),
					"drop_schema":        boolOptional("Whether to skip dropping schemas"),
					"modify_schema":      boolOptional("Whether to skip modifying schemas"),
					"add_table":          boolOptional("Whether to skip adding tables"),
					"drop_table":         boolOptional("Whether to skip dropping tables"),
					"modify_table":       boolOptional("Whether to skip modifying tables"),
					"add_column":         boolOptional("Whether to skip adding columns"),
					"drop_column":        boolOptional("Whether to skip dropping columns"),
					"modify_column":      boolOptional("Whether to skip modifying columns"),
					"add_index":          boolOptional("Whether to skip adding indexes"),
					"drop_index":         boolOptional("Whether to skip dropping indexes"),
					"modify_index":       boolOptional("Whether to skip modifying indexes"),
					"add_foreign_key":    boolOptional("Whether to skip adding foreign keys"),
					"drop_foreign_key":   boolOptional("Whether to skip dropping foreign keys"),
					"modify_foreign_key": boolOptional("Whether to skip modifying foreign keys"),
				},
			},
		},
	}
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
	resp.Diagnostics.Append(r.configure(req.ProviderData)...)
}

// GetSchema implements resource.Resource.
func (r *AtlasSchemaResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Atlas database resource manages the data schema of the database, " +
			"using an HCL file describing the wanted state of the database. " +
			"See https://atlasgo.io/",
		Blocks: map[string]schema.Block{
			"diff": diffBlock,
		},
		Attributes: map[string]schema.Attribute{
			"hcl": schema.StringAttribute{
				Description: "The schema definition for the database " +
					"(preferably normalized - see `atlas_schema` data source)",
				Required: true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"url": schema.StringAttribute{
				Description: "The url of the database see https://atlasgo.io/cli/url",
				Required:    true,
				Sensitive:   true,
			},
			"dev_url": schema.StringAttribute{
				Description: "The url of the dev-db see https://atlasgo.io/cli/url",
				Optional:    true,
				Sensitive:   true,
			},
			"exclude": schema.ListAttribute{
				Description: "Filter out resources matching the given glob pattern. See https://atlasgo.io/declarative/inspect#exclude-schemas",
				ElementType: types.StringType,
				Optional:    true,
			},
			"id": schema.StringAttribute{
				Description: "The ID of this resource",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"dev_db_url": schema.StringAttribute{
				Description: "Use `dev_url` instead.",
				Optional:    true,
				Sensitive:   true,
				DeprecationMessage: "This attribute is deprecated and will be removed in the next major version. " +
					"Please use the `dev_url` attribute instead.",
			},
		},
	}
}

// Create implements resource.Resource.
func (r *AtlasSchemaResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *AtlasSchemaResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(r.applySchema(ctx, data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.ID = types.StringValue(urlToID(data.URL))
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Read implements resource.Resource.
func (r *AtlasSchemaResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *AtlasSchemaResourceModel
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
		URL:     data.URL.ValueString(),
		Exclude: exclude,
		Format:  "hcl",
	})
	if err != nil {
		resp.Diagnostics.AddError("Inspect Error",
			fmt.Sprintf("Unable to inspect, got error: %s", err),
		)
		return
	}
	data.HCL = types.StringValue(hcl)
	data.ID = types.StringValue(urlToID(data.URL))
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update implements resource.Resource.
func (r *AtlasSchemaResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *AtlasSchemaResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(r.applySchema(ctx, data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete implements resource.Resource.
func (r *AtlasSchemaResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *AtlasSchemaResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Delete the resource by setting
	// the HCL to an empty string
	resp.Diagnostics.Append(emptySchema(ctx, data.URL.ValueString(), &data.HCL)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(r.applySchema(ctx, data)...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// ValidateConfig implements resource.ResourceWithValidateConfig.
func (r AtlasSchemaResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	resp.Diagnostics.Append(r.validateConfig(ctx, req.Config)...)
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
	if state == nil || state.HCL.ValueString() == "" {
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
		resp.Diagnostics.Append(emptySchema(ctx, plan.URL.ValueString(), &plan.HCL)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	resp.Diagnostics.Append(PrintPlanSQL(ctx, r.client, r.getDevURL(plan.DevURL, plan.DeprecatedDevURL), plan)...)
}

func PrintPlanSQL(ctx context.Context, c *atlas.Client, devURL string, data *AtlasSchemaResourceModel) (diags diag.Diagnostics) {
	d := &schemaData{
		Source: "schema.hcl",
		URL:    data.URL.ValueString(),
		DevURL: devURL,
		Diff:   data.Diff,
	}
	diags.Append(data.GetExclude(ctx, &d.Exclude)...)
	if diags.HasError() {
		return
	}
	dir, err := atlas.NewWorkingDir(atlas.WithAtlasHCL(d.render))
	if err != nil {
		diags.AddError("HCL Error",
			fmt.Sprintf("Unable to create working directory, got error: %s", err),
		)
		return
	}
	_, err = dir.WriteFile(d.Source, []byte(data.HCL.ValueString()))
	if err != nil {
		diags.AddError("HCL Error",
			fmt.Sprintf("Unable to create temporary file for HCL, got error: %s", err),
		)
		return
	}
	defer func() {
		if err := dir.Close(); err != nil {
			tflog.Debug(ctx, "Failed to cleanup working directory", map[string]any{
				"error": err,
			})
		}
	}()
	var result *atlas.SchemaApply
	err = c.WithWorkDir(dir.Path(), func(c *atlas.Client) (err error) {
		result, err = c.SchemaApply(ctx, &atlas.SchemaApplyParams{
			Env:    "tf",
			DryRun: true,
		})
		return err
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
	d := &schemaData{
		Source: "schema.hcl",
		URL:    data.URL.ValueString(),
		DevURL: r.getDevURL(data.DevURL, data.DeprecatedDevURL),
		Diff:   data.Diff,
	}
	diags.Append(data.GetExclude(ctx, &d.Exclude)...)
	if diags.HasError() {
		return
	}
	dir, err := atlas.NewWorkingDir(atlas.WithAtlasHCL(d.render))
	if err != nil {
		diags.AddError("HCL Error",
			fmt.Sprintf("Unable to create working directory, got error: %s", err),
		)
		return
	}
	_, err = dir.WriteFile(d.Source, []byte(data.HCL.ValueString()))
	if err != nil {
		diags.AddError("HCL Error",
			fmt.Sprintf("Unable to create temporary file for HCL, got error: %s", err),
		)
		return
	}
	defer func() {
		if err := dir.Close(); err != nil {
			tflog.Debug(ctx, "Failed to cleanup working directory", map[string]any{
				"error": err,
			})
		}
	}()
	err = r.client.WithWorkDir(dir.Path(), func(c *atlas.Client) error {
		_, err = c.SchemaApply(ctx, &atlas.SchemaApplyParams{
			Env: "tf",
		})
		return err
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
		DevURL:  r.getDevURL(data.DevURL),
		DryRun:  true,
		Exclude: exclude,
		To:      to,
		URL:     data.URL.ValueString(),
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

func urlToID(u types.String) string {
	uu, err := url.Parse(u.ValueString())
	if err != nil {
		return u.ValueString()
	}
	uu.User = nil
	return uu.String()
}

func (data *AtlasSchemaResourceModel) handleHCL() (string, func() error, error) {
	return atlas.TempFile(data.HCL.ValueString(), "hcl")
}

func (data *AtlasSchemaResourceModel) GetExclude(ctx context.Context, exclude *[]string) (diags diag.Diagnostics) {
	diags = data.Exclude.ElementsAs(ctx, exclude, false)
	if diags.HasError() {
		return
	}
	*exclude = nonEmptyStringSlice(*exclude)
	return
}

func boolOptional(desc string) schema.Attribute {
	return schema.BoolAttribute{
		Description: desc,
		Optional:    true,
	}
}
