package provider

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
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
	"github.com/hashicorp/terraform-plugin-log/tflog"

	atlas "ariga.io/atlas-go-sdk/atlasexec"
	"ariga.io/atlas/sql/migrate"
)

type (
	// MigrationResource defines the resource implementation.
	MigrationResource struct {
		providerData
	}
	// MigrationResourceModel describes the resource data model.
	MigrationResourceModel struct {
		URL    types.String `tfsdk:"url"`
		DevURL types.String `tfsdk:"dev_url"`

		DirURL          types.String `tfsdk:"dir"`
		RevisionsSchema types.String `tfsdk:"revisions_schema"`
		Version         types.String `tfsdk:"version"`
		Baseline        types.String `tfsdk:"baseline"`
		ExecOrder       types.String `tfsdk:"exec_order"`

		Cloud     *AtlasCloudBlock `tfsdk:"cloud"`
		RemoteDir *RemoteDirBlock  `tfsdk:"remote_dir"`

		EnvName types.String `tfsdk:"env_name"`
		Status  types.Object `tfsdk:"status"`
		ID      types.String `tfsdk:"id"`

		Timeouts timeouts.Value `tfsdk:"timeouts"`
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
func (r *MigrationResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The resource applies pending migration files on the connected database." +
			"See https://atlasgo.io/",
		Blocks: map[string]schema.Block{
			"cloud":      cloudBlock,
			"remote_dir": remoteDirBlock,
			"timeouts": timeouts.Block(ctx, timeouts.Opts{
				Create: true,
				Update: true,
				CreateDescription: `Timeout defaults to 20 mins. A string that can be [parsed as a duration](https://pkg.go.dev/time#ParseDuration) ` +
					`consisting of numbers and unit suffixes, such as "30s" or "2h45m". Valid time units are ` +
					`"s" (seconds), "m" (minutes), "h" (hours).`,
				UpdateDescription: `Timeout defaults to 20 mins. A string that can be [parsed as a duration](https://pkg.go.dev/time#ParseDuration) ` +
					`consisting of numbers and unit suffixes, such as "30s" or "2h45m". Valid time units are ` +
					`"s" (seconds), "m" (minutes), "h" (hours).`,
			}),
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
	createTimeout, diags := data.Timeouts.Create(ctx, 20*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()
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
	updateTimeout, diags := data.Timeouts.Update(ctx, 20*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()
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
		cfg, err := plan.projectConfig(r.cloud, r.devURL)
		if err != nil {
			resp.Diagnostics.AddError("Generate config failure",
				fmt.Sprintf("Failed to create atlas.hcl: %s", err.Error()))
			return
		}
		wd, err := atlas.NewWorkingDir(atlas.WithAtlasHCL(cfg.Render))
		if err != nil {
			resp.Diagnostics.AddError("Generate config failure",
				fmt.Sprintf("Failed to create temporary directory: %s", err.Error()))
			return
		}
		defer wd.Close()
		_ = r.client.WithWorkDir(wd.Path(), func(c *atlas.Client) error {
			report, err := c.MigrateStatus(ctx, &atlas.MigrateStatusParams{
				Env: cfg.EnvName,
			})
			if err != nil {
				resp.Diagnostics.AddError("Failed to read migration status", err.Error())
				return err
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
					return nil
				}
			}
			pendingCount, _ := report.Amount(plan.Version.ValueString())
			if pendingCount == 0 {
				return nil
			}
			if cfg.Env.DevURL == "" {
				// We don't have a dev URL, so we can't lint the migration
				return nil
			}
			lint, err := c.MigrateLint(ctx, &atlas.MigrateLintParams{
				Env:    cfg.EnvName,
				Latest: pendingCount,
			})
			if err != nil {
				resp.Diagnostics.AddError("Failed to lint migration", err.Error())
				return err
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
					resp.Diagnostics.AddWarning("Lint error",
						fmt.Sprintf("File: %s\n%s", f.Name, f.Error))
				}
			}
			return nil
		})
	}
}

const (
	StatePending  = "PENDING_USER"
	StateApproved = "APPROVED"
	StateAborted  = "ABORTED"
	StateApplied  = "APPLIED"
)

func (r *MigrationResource) migrate(ctx context.Context, data *MigrationResourceModel) (diags diag.Diagnostics) {
	cfg, err := data.projectConfig(r.cloud, r.devURL)
	if err != nil {
		diags.AddError("Generate config failure",
			fmt.Sprintf("Failed to create atlas.hcl: %s", err.Error()))
		return
	}
	wd, err := atlas.NewWorkingDir(atlas.WithAtlasHCL(cfg.Render))
	if err != nil {
		diags.AddError("Generate config failure",
			fmt.Sprintf("Failed to create temporary directory: %s", err.Error()))
		return
	}
	defer func() {
		if err := wd.Close(); err != nil {
			tflog.Debug(ctx, "Failed to cleanup working directory", map[string]any{
				"error": err,
			})
		}
	}()
	_ = r.client.WithWorkDir(wd.Path(), func(c *atlas.Client) error {
		toVersion := data.Version.ValueString()
		dirURL, err := cfg.Env.DirURL(wd, toVersion)
		if err != nil {
			diags.AddError("Generate config failure",
				fmt.Sprintf("Failed to create atlas.hcl: %s", err.Error()))
			return err
		}
		status, err := c.MigrateStatus(ctx, &atlas.MigrateStatusParams{
			DirURL: dirURL,
			Env:    cfg.EnvName,
		})
		if err != nil {
			diags.AddError("Failed to read migration status", err.Error())
			return err
		}
		switch {
		case len(status.Pending) == 0 && len(status.Applied) > 0 && len(status.Applied) > len(status.Available):
			params := &atlas.MigrateDownParams{
				Env:       cfg.EnvName,
				ToVersion: status.Available[len(status.Available)-1].Version,
				Context: &atlas.DeployRunContext{
					TriggerType:    atlas.TriggerTypeTerraform,
					TriggerVersion: r.version,
				},
			}
			params.DirURL, err = cfg.Env.DirURLLatest()
			if err != nil {
				diags.AddError("Generate config failure",
					fmt.Sprintf("Failed to parse migration directory URL: %s", err.Error()))
				return err
			}
		loop:
			for {
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(1 * time.Second):
					run, err := c.MigrateDown(ctx, params)
					if err != nil {
						diags.AddError("Failed to down migration", err.Error())
						return err
					}
					switch run.Status {
					case StatePending:
						diags.AddWarning("Down migration",
							fmt.Sprintf("Migration is waiting for approval, review here: %s", run.URL))
						continue
					case StateAborted:
						diags.AddError("Down migration",
							fmt.Sprintf("Migration was aborted, review here: %s", run.URL))
						return nil
					case StateApplied, StateApproved:
						break loop
					}
				}
			}
		case len(status.Pending) == 0:
			break
		default:
			amount, synced := status.Amount(toVersion)
			if !synced {
				if amount == 0 {
					diags.AddAttributeError(
						path.Root("version"),
						"Incorrect version",
						fmt.Sprintf("The version %s is not found in the pending migrations.", data.Version.ValueString()),
					)
					return nil
				}
				_, err := c.MigrateApply(ctx, &atlas.MigrateApplyParams{
					Env:    cfg.EnvName,
					Amount: amount,
					Context: &atlas.DeployRunContext{
						TriggerType:    atlas.TriggerTypeTerraform,
						TriggerVersion: r.version,
					},
				})
				if err != nil {
					diags.AddError("Failed to apply migrations", err.Error())
					return err
				}
			}
		}
		data.Status, diags = r.buildStatus(ctx, data)
		return nil
	})
	return diags
}

func (r *MigrationResource) buildStatus(ctx context.Context, data *MigrationResourceModel) (obj types.Object, diags diag.Diagnostics) {
	obj = types.ObjectNull(statusObjectAttrs)
	cfg, err := data.projectConfig(r.cloud, r.devURL)
	if err != nil {
		diags.AddError("Generate config failure",
			fmt.Sprintf("Failed to create atlas.hcl: %s", err.Error()))
		return
	}
	wd, err := atlas.NewWorkingDir(atlas.WithAtlasHCL(cfg.Render))
	if err != nil {
		diags.AddError("Generate config failure",
			fmt.Sprintf("Failed to create temporary directory: %s", err.Error()))
		return
	}
	defer func() {
		if err := wd.Close(); err != nil {
			tflog.Debug(ctx, "Failed to cleanup working directory", map[string]any{
				"error": err,
			})
		}
	}()
	_ = r.client.WithWorkDir(wd.Path(), func(c *atlas.Client) error {
		report, err := c.MigrateStatus(ctx, &atlas.MigrateStatusParams{
			Env: cfg.EnvName,
		})
		if err != nil {
			diags.AddError("Failed to read migration status", err.Error())
			return err
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
		obj, diags = types.ObjectValue(statusObjectAttrs, map[string]attr.Value{
			"status":  types.StringValue(report.Status),
			"current": current,
			"next":    next,
			"latest":  latest,
		})
		return nil
	})
	return
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

type (
	chunkedDir struct {
		migrate.Dir
		latestIndex int
	}
	// memFile implements fs.File.
	memFile struct {
		io.Reader
		fs.FileInfo
	}
)

var (
	_ fs.File     = &memFile{}
	_ migrate.Dir = (*chunkedDir)(nil)
)

// NewMemFile returns a new in-memory file.
func NewMemFile(f io.Reader) fs.File {
	return &memFile{Reader: f}
}

func (f *memFile) Close() error               { return nil }
func (f *memFile) Stat() (fs.FileInfo, error) { return f.FileInfo, nil }

// NewChunkedDir returns a new Dir that only contains migrations up to the
// given version. (inclusive)
//
// If version is empty, the original Dir is returned.
// If version is not found, an error is returned.
func NewChunkedDir(dir migrate.Dir, version string) (migrate.Dir, error) {
	if version == "" {
		return dir, nil
	}
	// Validate the directory first,
	// to ensure its checksum is correct.
	if err := migrate.Validate(dir); err != nil {
		return nil, err
	}
	files, err := dir.Files()
	if err != nil {
		return nil, err
	}
	idx := slices.IndexFunc(files, func(f migrate.File) bool {
		return f.Version() == version
	})
	if idx == -1 {
		return nil, fmt.Errorf("version %q not found", version)
	}
	// We want to include the version file, so add 1.
	return &chunkedDir{Dir: dir, latestIndex: idx + 1}, nil
}

// CommitID implements Dir.CommitID.
func (d *chunkedDir) CommitID() string {
	if dir, ok := d.Dir.(interface{ CommitID() string }); ok {
		return dir.CommitID()
	}
	return ""
}

// Open implements migrate.Dir.
//
// If the file is atlas.sum, we return a file with
// the checksum of chunked migration files.
func (d *chunkedDir) Open(name string) (fs.File, error) {
	if name != migrate.HashFileName {
		return d.Dir.Open(name)
	}
	sm, err := d.Checksum()
	if err != nil {
		return nil, err
	}
	data, err := sm.MarshalText()
	if err != nil {
		return nil, err
	}
	return NewMemFile(bytes.NewReader(data)), nil
}

// Path implements Dir.Path.
func (d *chunkedDir) Path() string {
	if dir, ok := d.Dir.(interface{ Path() string }); ok {
		return dir.Path()
	}
	return ""
}

// Checksum implements Dir.Checksum.
func (d *chunkedDir) Checksum() (migrate.HashFile, error) {
	files, err := d.Files()
	if err != nil {
		return nil, err
	}
	return migrate.NewHashFile(files)
}

// Files implements Dir.Files.
func (d *chunkedDir) Files() ([]migrate.File, error) {
	files, err := d.Dir.Files()
	if err != nil {
		return nil, err
	}
	return files[:d.latestIndex], nil
}

// Schema reader types (URL schemes).
const (
	SchemaTypeFile   = "file"
	SchemaTypeAtlas  = "atlas"
	SchemaTypeSQLite = "sqlite"
)

func (d *MigrationResourceModel) projectConfig(cloud *AtlasCloudBlock, devURL string) (*projectConfig, error) {
	dbURL, err := absoluteSqliteURL(d.URL.ValueString())
	if err != nil {
		return nil, err
	}
	cfg := projectConfig{
		Config:  baseAtlasHCL,
		EnvName: defaultString(d.EnvName, "tf"),
		Env: &envConfig{
			URL:    dbURL,
			DevURL: defaultString(d.DevURL, devURL),
			Migration: &migrationConfig{
				Baseline:        d.Baseline.ValueString(),
				RevisionsSchema: d.RevisionsSchema.ValueString(),
				ExecOrder:       d.ExecOrder.ValueString(),
			},
		},
	}
	if d.Cloud != nil && d.Cloud.Token.ValueString() != "" {
		// Use the resource's cloud block if it is set
		cloud = d.Cloud
	}
	if cloud != nil {
		cfg.Cloud = &cloudConfig{
			Token:   cloud.Token.ValueString(),
			Project: cloud.Project.ValueStringPointer(),
			URL:     cloud.URL.ValueStringPointer(),
		}
	}
	if rd := d.RemoteDir; rd != nil {
		if cfg.Cloud == nil {
			return nil, fmt.Errorf("cloud configuration is not set")
		}
		cfg.Env.Migration.DirURL, err = rd.AtlasURL()
	} else {
		cfg.Env.Migration.DirURL, err = absoluteFileURL(
			defaultString(d.DirURL, "migrations"))
	}
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}
