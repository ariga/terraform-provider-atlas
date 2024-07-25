package provider

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
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
		report, err := r.client.MigrateStatus(ctx, &atlas.MigrateStatusParams{
			ConfigURL: fmt.Sprintf("file://%s", filepath.ToSlash(cfgPath)),
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
		lint, err := r.client.MigrateLint(ctx, &atlas.MigrateLintParams{
			ConfigURL: fmt.Sprintf("file://%s", filepath.ToSlash(cfgPath)),
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

const (
	StatePending  = "PENDING_USER"
	StateApproved = "APPROVED"
	StateAborted  = "ABORTED"
	StateApplied  = "APPLIED"
)

func (r *MigrationResource) migrate(ctx context.Context, data *MigrationResourceModel) (diags diag.Diagnostics) {
	dir, err := os.MkdirTemp(os.TempDir(), "tf-atlas-*")
	if err != nil {
		diags.AddError("Generate config failure",
			fmt.Sprintf("Failed to create temporary directory: %s", err.Error()))
		return
	}
	defer os.RemoveAll(dir)
	cfgPath := filepath.Join(dir, "atlas.hcl")
	if data.Cloud != nil && data.Cloud.Token.ValueString() != "" {
		// Override cloud config from provider if cloud config from resource is set
		r.cloud = data.Cloud
	}
	err = data.AtlasHCL(cfgPath, r.devURL, r.cloud)
	if err != nil {
		diags.AddError("Generate config failure",
			fmt.Sprintf("Failed to create atlas.hcl: %s", err.Error()))
		return
	}
	var status *atlas.MigrateStatus
	switch {
	case r.cloud != nil && !data.RemoteDir.Name.IsNull():
		status, err = r.client.MigrateStatus(ctx, &atlas.MigrateStatusParams{
			ConfigURL: fmt.Sprintf("file://%s", filepath.ToSlash(cfgPath)),
			Env:       defaultString(data.EnvName, "tf"),
		})
		if err != nil {
			diags.AddError("Failed to read migration status", err.Error())
			return
		}
	default:
		dirURL, err := url.Parse(filepath.ToSlash(data.DirURL.ValueString()))
		if err != nil {
			diags.AddError("Generate config failure",
				fmt.Sprintf("Failed to parse migration directory URL: %s", err.Error()))
			return
		}
		switch f := dirURL.Query().Get("format"); strings.ToLower(f) {
		case "", "atlas":
		default:
			diags.AddError("Generate config failure",
				fmt.Sprintf("Unknown format for migration directory: %s", f))
			return
		}
		mdir, err := migrate.NewLocalDir(dirURL.Path)
		if err != nil {
			diags.AddError("Generate config failure",
				fmt.Sprintf("Failed to create a temporary directory for migration: %s", err.Error()))
			return
		}
		// in case of specifying a 'version' < latest, we need a new dir
		// that contains only the migrations up to the 'version'
		// helps getting the status of the migrations later
		cdir, err := NewChunkedDir(mdir, data.Version.ValueString())
		if err != nil {
			diags.AddError("Generate config failure",
				fmt.Sprintf("Failed to create chunked directory: %s", err.Error()))
			return
		}
		wd, err := atlas.NewWorkingDir(
			func(ce *atlas.WorkingDir) error {
				return ce.CopyFS(dirURL.Path, cdir)
			},
		)
		err = r.client.WithWorkDir(wd.Path(), func(c *atlas.Client) error {
			status, err = c.MigrateStatus(ctx, &atlas.MigrateStatusParams{
				ConfigURL: fmt.Sprintf("file://%s", filepath.ToSlash(cfgPath)),
				Env:       defaultString(data.EnvName, "tf"),
			})
			return err
		})
		if err != nil {
			diags.AddError("Failed to read migration status", err.Error())
			return
		}
	}
	var (
		downing   = len(status.Pending) == 0 && len(status.Applied) > 0 && len(status.Applied) > len(status.Available)
		noPending = len(status.Pending) == 0
	)
	switch {
	case downing:
		params := &atlas.MigrateDownParams{
			ConfigURL: fmt.Sprintf("file://%s", filepath.ToSlash(cfgPath)),
			Env:       defaultString(data.EnvName, "tf"),
			ToVersion: status.Available[len(status.Available)-1].Version,
			Context: &atlas.DeployRunContext{
				TriggerType:    atlas.TriggerTypeTerraform,
				TriggerVersion: r.version,
			},
		}
		switch {
		case r.cloud != nil && !data.RemoteDir.Name.IsNull():
			params.DirURL = fmt.Sprintf("atlas://%s", data.RemoteDir.Name.ValueString())
		default:
			params.DirURL = fmt.Sprintf("file://%s", data.DirURL.ValueString())
		}
	loop:
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(1 * time.Second):
				run, err := r.client.MigrateDown(ctx, params)
				if err != nil {
					diags.AddError("Failed to down migration", err.Error())
					return
				}
				switch run.Status {
				case StatePending:
					diags.AddWarning("Down migration",
						fmt.Sprintf("Migration is waiting for approval, review here: %s", run.URL))
					continue
				case StateAborted:
					diags.AddError("Down migration",
						fmt.Sprintf("Migration was aborted, review here: %s", run.URL))
					return
				case StateApplied, StateApproved:
					break loop
				}
			}
		}
	case noPending:
		break
	default:
		amount, synced := status.Amount(data.Version.ValueString())
		if !synced {
			if amount == 0 {
				diags.AddAttributeError(
					path.Root("version"),
					"Incorrect version",
					fmt.Sprintf("The version %s is not found in the pending migrations.", data.Version.ValueString()),
				)
				return
			}
			_, err := r.client.MigrateApply(ctx, &atlas.MigrateApplyParams{
				ConfigURL: fmt.Sprintf("file://%s", filepath.ToSlash(cfgPath)),
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
	report, err := r.client.MigrateStatus(ctx, &atlas.MigrateStatusParams{
		ConfigURL: fmt.Sprintf("file://%s", filepath.ToSlash(cfgPath)),
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

const migrationAtlasHCL = "env {\n  name = atlas.env\n}"

func (d *MigrationResourceModel) AtlasHCL(name string, devURL string, cloud *AtlasCloudBlock) error {
	cfg := atlasHCL{
		URL:    d.URL.ValueString(),
		DevURL: defaultString(d.DevURL, devURL),
		Migration: &migrationConfig{
			Baseline:        d.Baseline.ValueString(),
			RevisionsSchema: d.RevisionsSchema.ValueString(),
			ExecOrder:       d.ExecOrder.ValueString(),
		},
	}
	if cloud != nil {
		cfg.Cloud = &cloudConfig{
			Token:   cloud.Token.ValueString(),
			Project: cloud.Project.ValueStringPointer(),
			URL:     cloud.URL.ValueStringPointer(),
		}
	}
	switch {
	case d.RemoteDir != nil:
		if cfg.Cloud == nil {
			return fmt.Errorf("cloud configuration is not set")
		}
		cfg.Migration.DirURL = "atlas://" + d.RemoteDir.Name.ValueString()
		if !d.RemoteDir.Tag.IsNull() {
			cfg.Migration.DirURL += "?tag=" + d.RemoteDir.Tag.ValueString()
		}
	case !d.DirURL.IsNull():
		cfg.Migration.DirURL = fmt.Sprintf("file://%s", d.DirURL.ValueString())
	default:
		cfg.Migration.DirURL = "file://migrations"
	}
	return cfg.CreateFile(migrationAtlasHCL, name)
}
