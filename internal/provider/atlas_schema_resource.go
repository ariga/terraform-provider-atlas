package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/go-uuid"
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
		ProviderData
	}
	// AtlasSchemaResourceModel describes the resource data model.
	AtlasSchemaResourceModel struct {
		ID      types.String `tfsdk:"id"`
		HCL     types.String `tfsdk:"hcl"`
		URL     types.String `tfsdk:"url"`
		DevURL  types.String `tfsdk:"dev_url"`
		Exclude types.List   `tfsdk:"exclude"`
		TxMode  types.String `tfsdk:"tx_mode"`
		// Policies
		Diff *Diff `tfsdk:"diff"`
		Lint *Lint `tfsdk:"lint"`
		// Project config
		Config  types.String `tfsdk:"config"`
		Vars    types.String `tfsdk:"variables"`
		EnvName types.String `tfsdk:"env_name"`
	}
	// Diff defines the diff policies to apply when planning schema changes.
	Diff struct {
		ConcurrentIndex *ConcurrentIndex `tfsdk:"concurrent_index"`
		Skip            *SkipChanges     `tfsdk:"skip"`
	}
	// Lint defines the lint policies to apply when planning schema changes.
	Lint struct {
		Review types.String `tfsdk:"review"`
	}
	ConcurrentIndex struct {
		Create types.Bool `tfsdk:"create"`
		Drop   types.Bool `tfsdk:"drop"`
	}
	// SkipChanges represents the skip changes policy.
	SkipChanges struct {
		AddSchema        types.Bool `tfsdk:"add_schema"`
		DropSchema       types.Bool `tfsdk:"drop_schema"`
		ModifySchema     types.Bool `tfsdk:"modify_schema"`
		AddTable         types.Bool `tfsdk:"add_table"`
		DropTable        types.Bool `tfsdk:"drop_table"`
		ModifyTable      types.Bool `tfsdk:"modify_table"`
		AddColumn        types.Bool `tfsdk:"add_column"`
		DropColumn       types.Bool `tfsdk:"drop_column"`
		ModifyColumn     types.Bool `tfsdk:"modify_column"`
		AddIndex         types.Bool `tfsdk:"add_index"`
		DropIndex        types.Bool `tfsdk:"drop_index"`
		ModifyIndex      types.Bool `tfsdk:"modify_index"`
		AddForeignKey    types.Bool `tfsdk:"add_foreign_key"`
		DropForeignKey   types.Bool `tfsdk:"drop_foreign_key"`
		ModifyForeignKey types.Bool `tfsdk:"modify_foreign_key"`
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
	lintBlock = schema.SingleNestedBlock{
		Description: "The lint policy",
		Attributes: map[string]schema.Attribute{
			"review": schema.StringAttribute{
				Description: "The review policy",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("ALWAYS", "WARNING", "ERROR"),
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
			"lint": lintBlock,
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
				Optional:    true,
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
			"tx_mode": schema.StringAttribute{
				Description: "The transaction mode to use when applying the schema. See https://atlasgo.io/versioned/apply#transaction-configuration",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("file", "all", "none"),
				},
			},
			"id": schema.StringAttribute{
				Description: "The ID of this resource",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"config": schema.StringAttribute{
				Description: "The content of atlas.hcl config",
				Optional:    true,
				Sensitive:   false,
			},
			"variables": schema.StringAttribute{
				Description: "Stringify JSON object containing variables to be used inside the Atlas configuration file.",
				Optional:    true,
			},
			"env_name": schema.StringAttribute{
				Description: "The name of the environment used for reporting runs to Atlas Cloud. Default: tf",
				Optional:    true,
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
	id, err := uuid.GenerateUUID()
	if err != nil {
		resp.Diagnostics.AddError("UUID Error",
			fmt.Sprintf("Unable to generate UUID, got error: %s", err),
		)
		return
	}
	data.ID = types.StringValue(id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Read implements resource.Resource.
func (r *AtlasSchemaResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *AtlasSchemaResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(r.readSchema(ctx, data)...)
	if resp.Diagnostics.HasError() {
		return
	}
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
	w, cleanup, err := data.Workspace(ctx, &r.ProviderData)
	if err != nil {
		resp.Diagnostics.AddError("Generate config failure",
			fmt.Sprintf("Failed to create workspace: %s", err.Error()))
		return
	}
	defer cleanup()
	_, err = w.Exec.SchemaClean(ctx, &atlas.SchemaCleanParams{
		Env:         w.Project.EnvName,
		Vars:        w.Project.Vars,
		AutoApprove: true,
	})
	if err != nil {
		resp.Diagnostics.AddError("Apply Error",
			fmt.Sprintf("Unable to apply changes, got error: %s", err),
		)
		return
	}
}

// ValidateConfig implements resource.ResourceWithValidateConfig.
func (r AtlasSchemaResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var plan AtlasSchemaResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(r.validate(ctx, &plan)...)
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
	var isDelete bool
	if plan == nil {
		// This is a delete operation
		if state == nil {
			// This is a delete operation on a resource that doesn't exist
			// in the state, so we can safely ignore it
			return
		}
		plan = state.Clone()
		isDelete = true
	}
	resp.Diagnostics.Append(PrintPlanSQL(ctx, &r.ProviderData, plan, isDelete)...)
}

func PrintPlanSQL(ctx context.Context, p *ProviderData, data *AtlasSchemaResourceModel, delete bool) (diags diag.Diagnostics) {
	w, cleanup, err := data.Workspace(ctx, p)
	if err != nil {
		diags.AddError("Generate config failure",
			fmt.Sprintf("Failed to create workspace: %s", err.Error()))
		return
	}
	defer cleanup()
	var appliedFile *atlas.AppliedFile
	if delete {
		result, err := w.Exec.SchemaClean(ctx, &atlas.SchemaCleanParams{
			Env:    w.Project.EnvName,
			Vars:   w.Project.Vars,
			DryRun: true,
		})
		if err != nil {
			diags.AddError("Atlas Plan Error",
				fmt.Sprintf("Unable to generate migration plan, got error: %s", err),
			)
			return
		}
		appliedFile = result.Applied
	} else {
		result, err := w.Exec.SchemaApply(ctx, &atlas.SchemaApplyParams{
			Env:    w.Project.EnvName,
			Vars:   w.Project.Vars,
			TxMode: data.TxMode.ValueString(),
			DryRun: true,
		})
		if err != nil {
			diags.AddError("Atlas Plan Error",
				fmt.Sprintf("Unable to generate migration plan, got error: %s", err),
			)
			return
		}
		appliedFile = result.Applied

	}
	if appliedFile != nil && len(appliedFile.Applied) > 0 {
		buf := &strings.Builder{}
		for _, stmt := range appliedFile.Applied {
			fmt.Fprintln(buf, stmt)
		}
		diags.AddWarning("Atlas Plan",
			fmt.Sprintf("The following SQL statements will be executed:\n\n\n%s", buf.String()),
		)
	}
	return diags
}

func (r *AtlasSchemaResource) readSchema(ctx context.Context, data *AtlasSchemaResourceModel) (diags diag.Diagnostics) {
	w, cleanup, err := data.Workspace(ctx, &r.ProviderData)
	if err != nil {
		diags.AddError("Generate config failure",
			fmt.Sprintf("Failed to create workspace: %s", err.Error()))
		return
	}
	defer cleanup()
	hcl, err := w.Exec.SchemaInspect(ctx, &atlas.SchemaInspectParams{
		Env:  w.Project.EnvName,
		Vars: w.Project.Vars,
	})
	if err != nil {
		diags.AddError("Inspect Error",
			fmt.Sprintf("Unable to inspect, got error: %s", err),
		)
		return
	}
	// Set the HCL value
	data.HCL = types.StringValue(hcl)
	return
}

func (r *AtlasSchemaResource) applySchema(ctx context.Context, data *AtlasSchemaResourceModel) (diags diag.Diagnostics) {
	w, cleanup, err := data.Workspace(ctx, &r.ProviderData)
	if err != nil {
		diags.AddError("Generate config failure",
			fmt.Sprintf("Failed to create workspace: %s", err.Error()))
		return
	}
	defer cleanup()
	review, err := w.Project.LintReview()
	if err != nil {
		diags.AddError("Configuration Error",
			fmt.Sprintf("Unable to parse configuration, got error: %s", err),
		)
		return
	}
	autoApprove := review == nil
	_, err = w.Exec.SchemaApply(ctx, &atlas.SchemaApplyParams{
		Env:         w.Project.EnvName,
		Vars:        w.Project.Vars,
		TxMode:      data.TxMode.ValueString(),
		AutoApprove: autoApprove,
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
	w, cleanup, err := data.Workspace(ctx, &r.ProviderData)
	if err != nil {
		diags.AddError("Generate config failure",
			fmt.Sprintf("Failed to create workspace: %s", err.Error()))
		return
	}
	defer cleanup()
	review, err := w.Project.LintReview()
	if err != nil {
		diags.AddError("Configuration Error",
			fmt.Sprintf("Unable to parse configuration, got error: %s", err),
		)
		return
	}
	autoApprove := review == nil
	result, err := w.Exec.SchemaApply(ctx, &atlas.SchemaApplyParams{
		DryRun:      true,
		Env:         w.Project.EnvName,
		Vars:        w.Project.Vars,
		AutoApprove: autoApprove,
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

func (d *AtlasSchemaResourceModel) Workspace(ctx context.Context, p *ProviderData) (*Workspace, func(), error) {
	dbURL, err := absoluteSqliteURL(d.URL.ValueString())
	if err != nil {
		return nil, nil, err
	}
	cfg := &projectConfig{
		Config:  defaultString(d.Config, ""),
		Cloud:   cloudConfig(p.Cloud),
		EnvName: defaultString(d.EnvName, "tf"),
		Env: &envConfig{
			URL:    dbURL,
			DevURL: defaultString(d.DevURL, p.DevURL),
			Source: "file://schema.hcl",
			Diff:   d.Diff,
			Lint:   d.Lint,
		},
	}
	if cloud := p.Cloud; cloud.Valid() {
		cfg.Cloud = &CloudConfig{
			Token: cloud.Token.ValueString(),
		}
	}
	diags := d.Exclude.ElementsAs(ctx, &cfg.Env.Exclude, false)
	if diags.HasError() {
		return nil, nil, errors.New(diags.Errors()[0].Summary())
	}
	if vars := d.Vars.ValueString(); vars != "" {
		if err = json.Unmarshal([]byte(vars), &cfg.Vars); err != nil {
			return nil, nil, fmt.Errorf("failed to parse variables: %w", err)
		}
	}
	wd, err := atlas.NewWorkingDir(
		atlas.WithAtlasHCL(cfg.Render),
		func(ce *atlas.WorkingDir) error {
			_, err = ce.WriteFile("schema.hcl", []byte(d.HCL.ValueString()))
			return err
		},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create temporary directory: %w", err)
	}
	cleanup := func() {
		if err := wd.Close(); err != nil {
			tflog.Debug(ctx, "Failed to cleanup working directory", map[string]any{
				"error": err,
			})
		}
	}
	c, err := p.Client(wd.Path(), cfg.Cloud)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("failed to create client: %w", err)
	}
	return &Workspace{
		Dir:     wd,
		Exec:    c,
		Project: cfg,
	}, cleanup, nil
}

func boolOptional(desc string) schema.Attribute {
	return schema.BoolAttribute{
		Description: desc,
		Optional:    true,
	}
}

// deleteZero removes zero values from a slice.
func deleteZero[S ~[]E, E comparable](s S) S {
	var zero E
	return slices.DeleteFunc(s, func(e E) bool {
		return e == zero
	})
}
