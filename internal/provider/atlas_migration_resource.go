package provider

import (
	"context"
	"fmt"

	"ariga.io/ariga/terraform-provider-atlas/internal/atlas"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type (
	// MigrationResource defines the resource implementation.
	MigrationResource struct {
		client *atlas.Client
	}
	// MigrationResourceModel describes the resource data model.
	MigrationResourceModel struct {
		DirURL          types.String `tfsdk:"dir"`
		URL             types.String `tfsdk:"url"`
		RevisionsSchema types.String `tfsdk:"revisions_schema"`
		Version         types.String `tfsdk:"version"`

		Status types.Object `tfsdk:"status"`
		ID     types.String `tfsdk:"id"`
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
func (r *MigrationResource) GetSchema(ctx context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: "The resource applies pending migration files on the connected database." +
			"See https://atlasgo.io/",
		Attributes: map[string]tfsdk.Attribute{
			"dir": {
				Description: "the URL of the migration directory, by default it is file://migrations, " +
					"e.g a directory named migrations in the current working directory.",
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
			"revisions_schema": {
				Description: "The name of the schema the revisions table resides in",
				Type:        types.StringType,
				Optional:    true,
			},
			"version": {
				Description: "The version of the migration to apply, if not specified the latest version will be applied",
				Type:        types.StringType,
				Optional:    true,
				Computed:    true,
			},
			"status": {
				Description: "The status of the migration",
				Type: types.ObjectType{
					AttrTypes: statusObjectAttrs,
				},
				Computed: true,
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
	data.ID = dirToID(data.DirURL.Value)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Read implements resource.Resource.
func (r *MigrationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *MigrationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.buildStatus(ctx, data)
	if err != nil {
		resp.Diagnostics.Append(atlas.ErrorDiagnostic(err, "Failed to read migration status"))
		return
	}
	data.ID = dirToID(data.DirURL.Value)
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
	if data.Version.Value == "" {
		resp.Diagnostics.AddAttributeWarning(
			path.Root("version"),
			"version is unset",
			"We recommend that you use 'version' to specify a version of the migration to run.\n"+
				"If you don't specify a version, the latest version will be used when the resource being created.\n"+
				"For keeping the database schema up to date, you should use set the version to using the value from "+
				"`atlas_migrate_status.next` or `atlas_migrate_status.latest`\n",
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
		report, err := r.client.Status(ctx, &atlas.StatusParams{
			DirURL:          plan.DirURL.Value,
			URL:             plan.URL.Value,
			RevisionsSchema: plan.RevisionsSchema.Value,
		})
		if err != nil {
			resp.Diagnostics.Append(atlas.ErrorDiagnostic(err, "Failed to read migration status"))
			return
		}
		if plan.Version.IsNull() {
			v := report.LatestVersion()
			plan.Version = types.String{Value: v, Null: v == ""}
		}
		// TODO(giautm): Add Atlas-Lint to report the issues
	}
}

func (r *MigrationResource) migrate(ctx context.Context, data *MigrationResourceModel) (diags diag.Diagnostics) {
	statusReport, err := r.client.Status(ctx, &atlas.StatusParams{
		DirURL:          data.DirURL.Value,
		URL:             data.URL.Value,
		RevisionsSchema: data.RevisionsSchema.Value,
	})
	if err != nil {
		diags.Append(atlas.ErrorDiagnostic(err, "Failed to read migration status"))
		return
	}
	amount, synced := statusReport.Amount(data.Version.Value)
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
			DirURL:          data.DirURL.Value,
			URL:             data.URL.Value,
			RevisionsSchema: data.RevisionsSchema.Value,
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
	if err = r.buildStatus(ctx, data); err != nil {
		diags.Append(atlas.ErrorDiagnostic(err, "Failed to read migration status"))
		return
	}
	return
}

func (r *MigrationResource) buildStatus(ctx context.Context, data *MigrationResourceModel) error {
	report, err := r.client.Status(ctx, &atlas.StatusParams{
		DirURL:          data.DirURL.Value,
		URL:             data.URL.Value,
		RevisionsSchema: data.RevisionsSchema.Value,
	})
	if err != nil {
		return err
	}
	var (
		current types.String
		next    types.String
	)
	if report.Status == "PENDING" && report.Current == noMigration {
		current = types.String{Null: true}
	} else {
		current = types.String{Value: report.Current}
	}
	if report.Status == "OK" && report.Next == latestVersion {
		next = types.String{Null: true}
	} else {
		next = types.String{Value: report.Next}
	}
	latestVersion := report.LatestVersion()
	data.Status = types.Object{
		Attrs: map[string]attr.Value{
			"status":  types.String{Value: report.Status},
			"current": current,
			"next":    next,
			"latest":  types.String{Value: latestVersion, Null: latestVersion == ""},
		},
		AttrTypes: statusObjectAttrs,
	}
	return nil
}

func dirToID(dir string) types.String {
	return types.String{Value: fmt.Sprintf("file://%s", dir)}
}
