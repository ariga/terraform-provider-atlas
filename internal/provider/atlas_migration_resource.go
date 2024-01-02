package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

	atlas "ariga.io/atlas-go-sdk/atlasexec"
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
		Baseline        types.String `tfsdk:"baseline"`
		ExecOrder       types.String `tfsdk:"exec_order"`

		Cloud     *AtlasCloudBlock `tfsdk:"cloud"`
		RemoteDir *RemoteDirBlock  `tfsdk:"remote_dir"`

		EnvName types.String `tfsdk:"env_name"`
		Status  types.Object `tfsdk:"status"`
		ID      types.String `tfsdk:"id"`
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
		Blocks: map[string]schema.Block{
			"cloud":      cloudBlock,
			"remote_dir": remoteDirBlock,
		},
		Attributes: map[string]schema.Attribute{
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
			"baseline": schema.StringAttribute{
				Description: "An optional version to start the migration history from. See https://atlasgo.io/versioned/apply#existing-databases",
				Optional:    true,
			},
			"exec_order": schema.StringAttribute{
				Description: "How Atlas computes and executes pending migration files to the database. One of `linear`,`linear-skip` or `non-linear`. See https://atlasgo.io/versioned/apply#execution-order",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("linear", "linear-skip", "non-linear"),
				},
			},
			"revisions_schema": schema.StringAttribute{
				Description: "The name of the schema the revisions table resides in",
				Optional:    true,
			},
			"dir": schema.StringAttribute{
				Description: "the URL of the migration directory." +
					" dir or remote_dir block is required",
				Optional: true,
			},
			"env_name": schema.StringAttribute{
				Description: "The name of the environment used for reporting runs to Atlas Cloud. Default: tf",
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
	data.ID = dirToID(data.RemoteDir, data.DirURL)
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
	if data.RemoteDir == nil {
		// Local dir, validate config for dev-url
		resp.Diagnostics.Append(r.validateConfig(ctx, req.Config)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	// Validate the remote_dir block
	switch {
	case data.RemoteDir != nil:
		if data.RemoteDir.Name.IsNull() {
			resp.Diagnostics.AddError(
				"remote_dir.name is unset",
				"remote_dir.name is required when remote_dir is set",
			)
			return
		}
		// providerData.client is set when the provider is configured
		if data.Cloud == nil && (r.cloud == nil && r.providerData.client != nil) {
			resp.Diagnostics.AddError(
				"cloud is unset",
				"cloud is required when remote_dir is set",
			)
			return
		}
		if !data.DirURL.IsNull() {
			resp.Diagnostics.AddError(
				"dir is set",
				"dir is not allowed when remote_dir is set",
			)
			return
		}
		return
	case data.DirURL.IsNull():
		resp.Diagnostics.AddError(
			"dir is unset",
			"dir is required when remote_dir is unset",
		)
		return
	case !data.DirURL.IsUnknown() && data.DirURL.ValueString() == "":
		resp.Diagnostics.AddError(
			"dir is empty",
			"dir is required when remote_dir is unset",
		)
		return
	}
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
		dir, err := os.MkdirTemp(os.TempDir(), "tf-atlas-*")
		if err != nil {
			resp.Diagnostics.AddError("Generate config failure",
				fmt.Sprintf("Failed to create temporary directory: %s", err.Error()))
			return
		}
		defer os.RemoveAll(dir)
		cfgPath := filepath.Join(dir, "atlas.hcl")
		err = plan.AtlasHCL(cfgPath, r.devURL, r.cloud)
		if err != nil {
			resp.Diagnostics.AddError("Generate config failure",
				fmt.Sprintf("Failed to create atlas.hcl: %s", err.Error()))
			return
		}
		report, err := r.client.Status(ctx, &atlas.MigrateStatusParams{
			ConfigURL: fmt.Sprintf("file://%s", cfgPath),
			Env:       defaultString(plan.EnvName, "tf"),
		})
		if err != nil {
			resp.Diagnostics.AddError("Failed to read migration status", err.Error())
			return
		}
		if plan.Version.ValueString() == "" {
			v := report.LatestVersion()
			if v == "" {
				plan.Version = types.StringNull()
			} else {
				plan.Version = types.StringValue(v)
			}
			// Update plan if the user didn't specify a version
			resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("version"), v)...)
			if resp.Diagnostics.HasError() {
				return
			}
		}
		pendingCount, _ := report.Amount(plan.Version.ValueString())
		if pendingCount == 0 {
			return
		}
		devURL := r.getDevURL(plan.DevURL)
		if devURL == "" {
			return
		}
		lint, err := r.client.Lint(ctx, &atlas.MigrateLintParams{
			ConfigURL: fmt.Sprintf("file://%s", cfgPath),
			Env:       defaultString(plan.EnvName, "tf"),
			Latest:    pendingCount,
		})
		if err != nil {
			resp.Diagnostics.AddError("Failed to lint migration", err.Error())
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
	dir, err := os.MkdirTemp(os.TempDir(), "tf-atlas-*")
	if err != nil {
		diags.AddError("Generate config failure",
			fmt.Sprintf("Failed to create temporary directory: %s", err.Error()))
		return
	}
	defer os.RemoveAll(dir)
	cfgPath := filepath.Join(dir, "atlas.hcl")
	err = data.AtlasHCL(cfgPath, r.devURL, r.cloud)
	if err != nil {
		diags.AddError("Generate config failure",
			fmt.Sprintf("Failed to create atlas.hcl: %s", err.Error()))
		return
	}
	statusReport, err := r.client.Status(ctx, &atlas.MigrateStatusParams{
		ConfigURL: fmt.Sprintf("file://%s", cfgPath),
		Env:       defaultString(data.EnvName, "tf"),
	})
	if err != nil {
		diags.AddError("Failed to read migration status", err.Error())
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
		_, err := r.client.MigrateApply(ctx, &atlas.MigrateApplyParams{
			ConfigURL: fmt.Sprintf("file://%s", cfgPath),
			Env:       defaultString(data.EnvName, "tf"),
			Amount:    amount,
			Context: &atlas.DeployRunContext{
				TriggerType:    atlas.TriggerTypeTerraform,
				TriggerVersion: r.version,
			},
		})
		if err != nil {
			diags.AddError("Failed to apply migrations", err.Error())
			return
		}
	}
	data.Status, diags = r.buildStatus(ctx, data)
	return
}

func (r *MigrationResource) buildStatus(ctx context.Context, data *MigrationResourceModel) (obj types.Object, diags diag.Diagnostics) {
	obj = types.ObjectNull(statusObjectAttrs)

	dir, err := os.MkdirTemp(os.TempDir(), "tf-atlas-*")
	if err != nil {
		diags.AddError("Generate config failure", fmt.Sprintf("Failed to create temporary directory: %s", err.Error()))
		return
	}
	defer os.RemoveAll(dir)
	cfgPath := filepath.Join(dir, "atlas.hcl")
	err = data.AtlasHCL(cfgPath, r.devURL, r.cloud)
	if err != nil {
		diags.AddError("Generate config failure", fmt.Sprintf("Failed to create atlas.hcl: %s", err.Error()))
		return
	}
	report, err := r.client.Status(ctx, &atlas.MigrateStatusParams{
		ConfigURL: fmt.Sprintf("file://%s", cfgPath),
		Env:       defaultString(data.EnvName, "tf"),
	})
	if err != nil {
		diags.AddError("Failed to read migration status", err.Error())
		return
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

// dirToID returns the ID of the resource.
func dirToID(remoteDir *RemoteDirBlock, dir types.String) types.String {
	if remoteDir != nil {
		return types.StringValue(fmt.Sprintf("remote_dir://%s", remoteDir.Name.ValueString()))
	}
	return types.StringValue(fmt.Sprintf("file://%s", dir.ValueString()))
}

func defaultString(s types.String, def string) string {
	if s.IsNull() || s.IsUnknown() {
		return def
	}
	return s.ValueString()
}

func (d *MigrationResourceModel) AtlasHCL(name string, devURL string, cloud *AtlasCloudBlock) error {
	cfg := templateData{
		URL:             d.URL.ValueString(),
		DevURL:          defaultString(d.DevURL, devURL),
		DirURL:          d.DirURL.ValueStringPointer(),
		Baseline:        d.Baseline.ValueString(),
		RevisionsSchema: d.RevisionsSchema.ValueString(),
		ExecOrder:       d.ExecOrder.ValueString(),
	}
	if d.Cloud != nil && d.Cloud.Token.ValueString() != "" {
		// Use the data source cloud block if it is set
		cloud = d.Cloud
	}
	if cloud != nil {
		cfg.Cloud = &cloudConfig{
			Token:   cloud.Token.ValueString(),
			Project: cloud.Project.ValueStringPointer(),
			URL:     cloud.URL.ValueStringPointer(),
		}
	}
	if d := d.RemoteDir; d != nil {
		cfg.RemoteDir = &remoteDir{
			Name: d.Name.ValueString(),
			Tag:  d.Tag.ValueStringPointer(),
		}
	}
	return cfg.CreateFile(name)
}
