package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"ariga.io/ariga/terraform-provider-atlas/internal/atlas"
)

type (
	// MigrationResource defines the resource implementation.
	MigrationResource struct {
		providerData
	}
	// MigrationResourceModel describes the resource data model.
	MigrationResourceModel struct {
		DirURL          types.String `tfsdk:"dir"`
		URL             types.String `tfsdk:"url"`
		DevURL          types.String `tfsdk:"dev_url"`
		RevisionsSchema types.String `tfsdk:"revisions_schema"`
		Version         types.String `tfsdk:"version"`

		Status types.Object `tfsdk:"status"`
		ID     types.String `tfsdk:"id"`
	}
	MigrationStatus struct {
		Status  types.String `tfsdk:"status"`
		Current types.String `tfsdk:"current"`
		Next    types.String `tfsdk:"next"`
		Latest  types.String `tfsdk:"latest"`
	}
)

// Ensure provider defined types fully satisfy framework interfaces
var (
	_ resource.Resource                   = &MigrationResource{}
	_ resource.ResourceWithModifyPlan     = &MigrationResource{}
	_ resource.ResourceWithConfigure      = &MigrationResource{}
	_ resource.ResourceWithValidateConfig = &MigrationResource{}
)

var (
	statusObjectAttrs = map[string]attr.Type{
		"status":  types.StringType,
		"current": types.StringType,
		"next":    types.StringType,
		"latest":  types.StringType,
	}
)

// NewMigrationResource returns a new MigrateResource.
func NewMigrationResource() resource.Resource {
	return &MigrationResource{}
}

// Metadata implements resource.Resource.
func (r *MigrationResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_migration"
}

func (r *MigrationResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	resp.Diagnostics.Append(r.configure(req.ProviderData)...)
}

// GetSchema implements resource.Resource.
func (r *MigrationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The resource applies pending migration files on the connected database." +
			"See https://atlasgo.io/",
		Attributes: map[string]schema.Attribute{
			"dir": schema.StringAttribute{
				Description: "the URL of the migration directory, by default it is file://migrations, " +
					"e.g a directory named migrations in the current working directory.",
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
			"revisions_schema": schema.StringAttribute{
				Description: "The name of the schema the revisions table resides in",
				Optional:    true,
			},
			"version": schema.StringAttribute{
				Description: "The version of the migration to apply, if not specified the latest version will be applied",
				Optional:    true,
				Computed:    true,
			},
			"status": schema.ObjectAttribute{
				Description:    "The status of the migration",
				AttributeTypes: statusObjectAttrs,
				Computed:       true,
			},
			"id": schema.StringAttribute{
				Description: "The ID of this resource",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

// Create implements resource.Resource.
func (r *MigrationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *MigrationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(r.migrate(ctx, data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Only set ID when creating a new resource
	data.ID = dirToID(data.DirURL)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Read implements resource.Resource.
func (r *MigrationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *MigrationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var status MigrationStatus
	resp.Diagnostics.Append(data.Status.As(ctx, &status, basetypes.ObjectAsOptions{})...)
	if resp.Diagnostics.HasError() {
		return
	}
	nextStatus, diags := r.buildStatus(ctx, data)
	if diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}
	var nextStatusObj MigrationStatus
	resp.Diagnostics.Append(nextStatus.As(ctx, &nextStatusObj, basetypes.ObjectAsOptions{})...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !status.Current.IsNull() && nextStatusObj.Current.IsNull() {
		// The resource has been deleted
		resp.State.RemoveResource(ctx)
		return
	}
	data.ID = dirToID(data.DirURL)
	data.Status = nextStatus
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update implements resource.Resource.
func (r *MigrationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *MigrationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(r.migrate(ctx, data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete implements resource.Resource.
func (r *MigrationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// No-op, because we don't want to delete the database
}

// Validate implements resource.ResourceWithValidateConfig.
func (r MigrationResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data MigrationResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(r.validateConfig(ctx, req.Config)...)
	if data.Version.IsNull() {
		resp.Diagnostics.AddAttributeWarning(
			path.Root("version"),
			"version is unset",
			"We recommend that you use 'version' to specify a version of the migration to run.\n"+
				"If you don't specify a version, the latest version will be used when the resource being created.\n"+
				"For keeping the database schema up to date, you should use set the version to using the value from "+
				"`atlas_migration.next` or `atlas_migration.latest`\n",
		)
	}
}

// ModifyPlan implements resource.ResourceWithModifyPlan.
func (r *MigrationResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	var plan *MigrationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var state *MigrationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan != nil {
		if plan.DirURL.IsUnknown() || plan.URL.IsUnknown() {
			return
		}
		report, err := r.client.Status(ctx, &atlas.StatusParams{
			DirURL:          plan.DirURL.ValueString(),
			URL:             plan.URL.ValueString(),
			RevisionsSchema: plan.RevisionsSchema.ValueString(),
		})
		if err != nil {
			resp.Diagnostics.Append(atlas.ErrorDiagnostic(err, "Failed to read migration status"))
			return
		}
		if plan.Version.ValueString() == "" {
			v := report.LatestVersion()
			if v == "" {
				plan.Version = types.StringNull()
			} else {
				plan.Version = types.StringValue(v)
			}
		}
		devURL := r.getDevURL(plan.DevURL)
		if devURL == "" {
			return
		}
		pendingCount, _ := report.Amount(plan.Version.ValueString())
		if pendingCount == 0 {
			return
		}
		lint, err := r.client.Lint(ctx, &atlas.LintParams{
			DirURL: plan.DirURL.ValueString(),
			DevURL: devURL,
			Latest: pendingCount,
		})
		if err != nil {
			resp.Diagnostics.Append(atlas.ErrorDiagnostic(err, "Failed to lint migration"))
			return
		}
		for _, f := range lint.Files {
			switch {
			case len(f.Reports) > 0:
				for _, r := range f.Reports {
					lintDiags := []string{fmt.Sprintf("File: %s\n%s", f.Name, f.Error)}
					for _, l := range r.Diagnostics {
						lintDiags = append(lintDiags, fmt.Sprintf("- %s: %s", l.Code, l.Text))
					}
					resp.Diagnostics.AddWarning(
						r.Text,
						strings.Join(lintDiags, "\n"),
					)
				}
			case f.Error != "":
				resp.Diagnostics.AddWarning("Lint error", fmt.Sprintf("File: %s\n%s", f.Name, f.Error))
			}
		}
	}
}

func (r *MigrationResource) migrate(ctx context.Context, data *MigrationResourceModel) (diags diag.Diagnostics) {
	statusReport, err := r.client.Status(ctx, &atlas.StatusParams{
		DirURL:          data.DirURL.ValueString(),
		URL:             data.URL.ValueString(),
		RevisionsSchema: data.RevisionsSchema.ValueString(),
	})
	if err != nil {
		diags.Append(atlas.ErrorDiagnostic(err, "Failed to read migration status"))
		return
	}
	amount, synced := statusReport.Amount(data.Version.ValueString())
	if !synced {
		if amount == 0 {
			diags.AddAttributeError(
				path.Root("version"),
				"Incorrect version",
				"The version is not found in the pending migrations.",
			)
			return
		}
		report, err := r.client.Apply(ctx, &atlas.ApplyParams{
			DirURL:          data.DirURL.ValueString(),
			URL:             data.URL.ValueString(),
			RevisionsSchema: data.RevisionsSchema.ValueString(),
			Amount:          amount,
		})
		if err != nil {
			diags.Append(atlas.ErrorDiagnostic(err, "Failed to apply migrations"))
			return
		}
		if report.Error != "" {
			diags.AddError("Failed to apply migration", report.Error)
			return
		}
	}
	data.Status, diags = r.buildStatus(ctx, data)
	return
}

func (r *MigrationResource) buildStatus(ctx context.Context, data *MigrationResourceModel) (types.Object, diag.Diagnostics) {
	report, err := r.client.Status(ctx, &atlas.StatusParams{
		DirURL:          data.DirURL.ValueString(),
		URL:             data.URL.ValueString(),
		RevisionsSchema: data.RevisionsSchema.ValueString(),
	})
	if err != nil {
		return types.ObjectNull(statusObjectAttrs), diag.Diagnostics{
			atlas.ErrorDiagnostic(err, "Failed to read migration status"),
		}
	}
	current := types.StringNull()
	if !(report.Status == "PENDING" && report.Current == noMigration) {
		current = types.StringValue(report.Current)
	}
	next := types.StringNull()
	if !(report.Status == "OK" && report.Next == latestVersion) {
		next = types.StringValue(report.Next)
	}
	latest := types.StringNull()
	if v := report.LatestVersion(); v != "" {
		latest = types.StringValue(v)
	}
	return types.ObjectValue(statusObjectAttrs, map[string]attr.Value{
		"status":  types.StringValue(report.Status),
		"current": current,
		"next":    next,
		"latest":  latest,
	})
}

func dirToID(dir types.String) types.String {
	return types.StringValue(fmt.Sprintf("file://%s", dir.ValueString()))
}
