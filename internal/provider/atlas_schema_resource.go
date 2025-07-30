package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/hashicorp/go-uuid"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	tfpath "github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"ariga.io/atlas-go-sdk/atlasexec"
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
		// Cloud config
		Cloud *AtlasCloudBlock `tfsdk:"cloud"`
	}
	// Diff defines the diff policies to apply when planning schema changes.
	Diff struct {
		ConcurrentIndex *ConcurrentIndex `tfsdk:"concurrent_index"`
		Skip            *SkipChanges     `tfsdk:"skip"`
	}
	// Lint defines the lint policies to apply when planning schema changes.
	Lint struct {
		Review types.String `tfsdk:"review"`
		// ReviewTimeout defines the wait time for the review to be approved.
		ReviewTimeout types.String `tfsdk:"review_timeout"`
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
				Description: "The review policy. One of `ALWAYS`, `WARNING` and `ERROR`.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("ALWAYS", "WARNING", "ERROR"),
				},
			},
			"review_timeout": schema.StringAttribute{
				Description: "The review timeout. The time to wait for the review to be approved. " +
					"Valid time unit are 's' (seconds), 'm' (minutes), 'h' (hours).",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(regexp.MustCompile(`^\d+[smhd]$`), "Must be a valid duration (e.g. 1m, 2h)"),
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
			"diff":  diffBlock,
			"lint":  lintBlock,
			"cloud": cloudBlock,
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
// ModifyPlan is a method of AtlasSchemaResource that modifies the Terraform resource plan
// before it is applied. It performs various checks and adjustments to ensure the plan is
// valid and safe to execute.
//
// Parameters:
// - ctx: The context for the operation, used for cancellation and deadlines.
// - req: The ModifyPlanRequest containing the current state and desired plan of the resource.
// - resp: The ModifyPlanResponse used to append diagnostics and indicate required changes.
//
// Behavior:
//   - Retrieves the desired plan and current state of the resource.
//   - If the current state is nil or the HCL field is empty, it checks if the plan requires
//     replacement or performs a first-run check to prevent accidental schema drops.
//   - Handles delete operations by cloning the state into the plan and marking it as a delete operation.
//   - Calls PrintPlanSQL to generate and log the SQL representation of the plan, appending any diagnostics.
//   - If warnings are detected in the diagnostics, it triggers a schema review process.
//
// This method ensures that the resource plan is consistent, safe, and adheres to the expected
// behavior of the Terraform provider.
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
			resp.RequiresReplace = append(resp.RequiresReplace, tfpath.Root("url"))
			return
		}
		// New terraform resource will be created,
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
	resp.Diagnostics.Append(r.reviewSchema(ctx, plan)...)
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
	repoURL, err := w.Project.RepoURL()
	if err != nil {
		diags.AddError("Failed to retrieve repo URL",
			fmt.Sprintf("Error getting repo URL: %s", err),
		)
		return
	}
	var planURL string
	// The approval flow is only enabled when repoURL is configured to connect to Atlas Cloud.
	// If repoURL is not set, the flow is disabled, and changes are applied directly.
	if review != nil && repoURL != nil {
		targetURL, err := w.Project.TargetURL()
		if err != nil {
			diags.AddError("Failed to retrieve target URL",
				fmt.Sprintf("Error getting target URL: %s", err),
			)
			return
		}
		timeout := time.Duration(0)
		if data.Lint != nil && data.Lint.ReviewTimeout.ValueString() != "" {
			timeoutStr := data.Lint.ReviewTimeout.ValueString()
			timeout, err = time.ParseDuration(timeoutStr)
			if err != nil {
				diags.AddError("Invalid review timeout format",
					fmt.Sprintf("Invalid review timeout format '%s': %s", timeoutStr, err),
				)
				return
			}
		}
		now := time.Now()
		for {
			// Check context cancellation
			if ctx.Err() != nil {
				diags.AddError("Waiting for plan approval",
					fmt.Sprintf("Context was cancelled: %s", ctx.Err()),
				)
				return
			}
			plans, err := w.Exec.SchemaPlanList(ctx, &atlas.SchemaPlanListParams{
				Env:  w.Project.EnvName,
				Vars: w.Project.Vars,
				Repo: repoURL.String(),
				From: []string{"env://url"},
				To:   []string{targetURL},
			})
			if err != nil {
				diags.AddError("Failed to list schema plans",
					fmt.Sprintf("Couldn't retrieve schema plans: %s", err),
				)
				return
			} else if len(plans) != 1 {
				break
			} else if len(plans) == 1 && plans[0].Status == "APPROVED" {
				planURL = plans[0].URL
				break
			} else if len(plans) == 1 && plans[0].Status == "PENDING" {
				if timeout == 0 {
					diags.AddError("Plan Pending Approval",
						fmt.Sprintf("The schema plan is awaiting approval. Please review and approve it and run again:\n\n%s", plans[0].Link),
					)
					return
				}
				if time.Since(now) > timeout {
					diags.AddError("Plan Approval Timeout",
						fmt.Sprintf("The schema plan is pending approval for too long. Please approve it and run again:\n\n%s", plans[0].Link),
					)
					return
				}
			}
			// Sleep for a second before checking again
			time.Sleep(1 * time.Second)
		}
	}
	if _, err = w.Exec.SchemaApply(ctx, &atlas.SchemaApplyParams{
		Env:         w.Project.EnvName,
		Vars:        w.Project.Vars,
		TxMode:      data.TxMode.ValueString(),
		AutoApprove: review == nil,
		PlanURL:     planURL,
	}); err != nil {
		diags.AddError("Apply Error",
			fmt.Sprintf("Unable to apply changes, got error: %s", err),
		)
		return
	}
	return
}

func (r *AtlasSchemaResource) reviewSchema(ctx context.Context, data *AtlasSchemaResourceModel) (diags diag.Diagnostics) {
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
	repoURL, err := w.Project.RepoURL()
	if err != nil {
		diags.AddError("Failed to retrieve repo URL",
			fmt.Sprintf("Error getting repo URL: %s", err),
		)
		return
	}
	if review == nil || repoURL == nil {
		return
	}
	targetURL, err := w.Project.TargetURL()
	if err != nil {
		diags.AddError("Failed to retrieve target URL",
			fmt.Sprintf("Error getting target URL: %s", err),
		)
		return
	}
	// Only run the review flow if there have a detected change
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
	if result.Applied == nil || (result.Applied != nil && len(result.Applied.Applied) == 0) {
		diags.AddWarning("No changes detected",
			"The schema has no changes to apply.",
		)
		return
	}
	createApprovalPlan := func() (*atlas.SchemaPlanFile, error) {
		// If the desired state is a file, we need to push the schema to the Atlas Cloud.
		// This is to ensure that the schema is in sync with the Atlas Cloud.
		// And the schema is available for the Atlas CLI (on local machine)
		// to modify or approve the changes.
		tag, err := w.Exec.SchemaInspect(ctx, &atlasexec.SchemaInspectParams{
			Env:    w.Project.EnvName,
			Vars:   w.Project.Vars,
			URL:    targetURL,
			Format: `{{ .Hash | base64url }}`,
		})
		if err != nil {
			return nil, fmt.Errorf("unable to inspect schema, got error: %w", err)
		}
		state, err := w.Exec.SchemaPush(ctx, &atlasexec.SchemaPushParams{
			Env:  w.Project.EnvName,
			Vars: w.Project.Vars,
			Name: path.Join(repoURL.Host, repoURL.Path),
			Tag:  fmt.Sprintf("terraform-plan-%.8s", strings.ToLower(tag)),
			URL:  []string{targetURL},
		})
		if err != nil {
			return nil, fmt.Errorf("unable to push schema, got error: %w", err)
		}
		targetURL = state.URL
		plan, err := w.Exec.SchemaPlan(ctx, &atlas.SchemaPlanParams{
			Env:     w.Project.EnvName,
			Vars:    w.Project.Vars,
			Repo:    repoURL.String(),
			From:    []string{"env://url"},
			To:      []string{targetURL},
			Pending: true,
		})
		if err != nil {
			return nil, fmt.Errorf("unable to create plan, got error: %w", err)
		}
		return plan.File, nil
	}
	// List all existing plans for this schema change
	var plan *atlas.SchemaPlanFile
	plans, err := w.Exec.SchemaPlanList(ctx, &atlas.SchemaPlanListParams{
		Env:  w.Project.EnvName,
		Vars: w.Project.Vars,
		Repo: repoURL.String(),
		From: []string{"env://url"},
		To:   []string{targetURL},
	})
	switch {
	case err != nil:
		diags.AddError("Failed to list schema plans",
			fmt.Sprintf("Couldn't retrieve schema plans: %s", err),
		)
		return
	// Review policy "ALWAYS": Always require manual approval regardless of changes
	case len(plans) == 0 && *review == "ALWAYS":
		plan, err = createApprovalPlan()
		if err != nil {
			diags.AddError("Failed to create approval plan",
				fmt.Sprintf("Couldn't create a schema plan for approval: %s", err),
			)
			return
		}
	// Review policies "WARNING" or "ERROR": Try direct apply first, but if rejected by policy,
	// create a plan for manual approval instead
	case len(plans) == 0 && (*review == "WARNING" || *review == "ERROR"):
		tmpPlan, err := w.Exec.SchemaPlan(ctx, &atlas.SchemaPlanParams{
			Env:    w.Project.EnvName,
			Vars:   w.Project.Vars,
			Repo:   repoURL.String(),
			From:   []string{"env://url"},
			To:     []string{targetURL},
			DryRun: true,
		})
		if err != nil {
			diags.AddError("Failed to plan schema changes",
				fmt.Sprintf("Failed to plan schema changes: %s", err),
			)
			return
		}
		if tmpPlan.Lint == nil {
			return
		}
		needApproval := false
		switch *review {
		case "WARNING":
			needApproval = tmpPlan.Lint.DiagnosticsCount() > 0
		case "ERROR":
			needApproval = len(tmpPlan.Lint.Errors()) > 0
		}
		if !needApproval {
			return
		}
		// Create a new plan for approval
		plan, err = createApprovalPlan()
		if err != nil {
			diags.AddError("Failed to create approval plan",
				fmt.Sprintf("Couldn't create a schema plan for approval after review policy rejection: %s", err),
			)
			return
		}
	case len(plans) == 1:
		plan = &plans[0]
	// Multiple plans for the same change detected - this is an error state
	case len(plans) > 1:
		planURLs := make([]string, 0, len(plans))
		for _, p := range plans {
			planURLs = append(planURLs, p.URL)
		}
		diags.AddError("Multiple schema plans detected",
			fmt.Sprintf("Found multiple plans for the same schema changes. Please remove old plans before applying:\n\n- %s",
				strings.Join(planURLs, "\n- ")),
		)
	}
	if plan != nil && plan.Status == "PENDING" {
		diags.AddWarning("Plan Pending Approval",
			fmt.Sprintf("The schema plan is awaiting approval. Please review and approve it before proceeding:\n\n%s", plan.Link),
		)
		return
	}
	return
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
		Cloud:   cloudConfig(d.Cloud, p.Cloud),
		EnvName: defaultString(d.EnvName, "tf"),
		Env: &envConfig{
			URL:    dbURL,
			DevURL: defaultString(d.DevURL, p.DevURL),
			Source: "file://schema.hcl",
			Diff:   d.Diff,
			Lint:   d.Lint,
			Schema: &schemaConfig{
				Repo: repoConfig(d.Cloud, p.Cloud),
			},
		},
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
