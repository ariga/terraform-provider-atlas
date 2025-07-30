package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path"

	"github.com/hashicorp/go-uuid"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	atlas "ariga.io/atlas-go-sdk/atlasexec"
)

type (
	// MigrationDataSource defines the data source implementation.
	MigrationDataSource struct {
		ProviderData
	}
	// MigrationDataSourceModel describes the data source data model.
	MigrationDataSourceModel struct {
		Config          types.String `tfsdk:"config"`
		Vars            types.String `tfsdk:"variables"`
		URL             types.String `tfsdk:"url"`
		DevURL          types.String `tfsdk:"dev_url"`
		RevisionsSchema types.String `tfsdk:"revisions_schema"`

		DirURL    types.String     `tfsdk:"dir"`
		Cloud     *AtlasCloudBlock `tfsdk:"cloud"`
		RemoteDir *RemoteDirBlock  `tfsdk:"remote_dir"`
		EnvName   types.String     `tfsdk:"env_name"`

		Status  types.String `tfsdk:"status"`
		Current types.String `tfsdk:"current"`
		Next    types.String `tfsdk:"next"`
		Latest  types.String `tfsdk:"latest"`
		ID      types.String `tfsdk:"id"`
	}
	RemoteDirBlock struct {
		Name types.String `tfsdk:"name"`
		Tag  types.String `tfsdk:"tag"`
	}
)

// Ensure provider defined types fully satisfy framework interfaces
var (
	_ datasource.DataSource                   = &MigrationDataSource{}
	_ datasource.DataSourceWithConfigure      = &MigrationDataSource{}
	_ datasource.DataSourceWithValidateConfig = &MigrationDataSource{}
)
var (
	latestVersion  = "Already at latest version"
	noMigration    = "No migration applied yet"
	remoteDirBlock = schema.SingleNestedBlock{
		DeprecationMessage: "Use the dir attribute with (atlas://<name>?tag=<tag>) URL format",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Description: "The name of the remote directory. This attribute is required when remote_dir is set",
				Optional:    true,
			},
			"tag": schema.StringAttribute{
				Description: "The tag of the remote directory",
				Optional:    true,
			},
		},
	}
)

// NewMigrationDataSource returns a new AtlasSchemaDataSource.
func NewMigrationDataSource() datasource.DataSource {
	return &MigrationDataSource{}
}

// Metadata implements datasource.DataSource.
func (d *MigrationDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_migration"
}

// Configure implements datasource.DataSourceWithConfigure.
func (d *MigrationDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	resp.Diagnostics.Append(d.configure(req.ProviderData)...)
}

// Validate implements resource.ResourceWithValidateConfig.
func (r MigrationDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var data MigrationDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if data.Config.ValueString() != "" && !data.EnvName.IsUnknown() && data.EnvName.ValueString() == "" {
		resp.Diagnostics.AddError(
			"env_name is empty",
			"env_name is required when config is set",
		)
		return
	}
	resp.Diagnostics.Append(r.validate(ctx, &data)...)
}

// GetSchema implements datasource.DataSource.
func (d *MigrationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Data source returns the information about the current migration.",
		Blocks: map[string]schema.Block{
			"cloud":      cloudBlock,
			"remote_dir": remoteDirBlock,
		},
		Attributes: map[string]schema.Attribute{
			"config": schema.StringAttribute{
				Description: "The configuration file for the migration",
				Optional:    true,
				Sensitive:   false,
			},
			"variables": schema.StringAttribute{
				Description: "Stringify JSON object containing variables to be used inside the Atlas configuration file.",
				Optional:    true,
			},
			"url": schema.StringAttribute{
				Description: "[driver://username:password@address/dbname?param=value] select a resource using the URL format",
				Optional:    true,
				Sensitive:   true,
			},
			"dev_url": schema.StringAttribute{
				Description: "The URL of the dev-db. See https://atlasgo.io/cli/url",
				Optional:    true,
				Sensitive:   true,
			},
			"dir": schema.StringAttribute{
				Description: "Select migration directory using URL format",
				Optional:    true,
			},
			"env_name": schema.StringAttribute{
				Description: "The name of the environment used for reporting runs to Atlas Cloud. Default: tf",
				Optional:    true,
			},
			"revisions_schema": schema.StringAttribute{
				Description: "The name of the schema the revisions table resides in",
				Optional:    true,
			},
			"status": schema.StringAttribute{
				Description: "The Status of migration (OK, PENDING)",
				Computed:    true,
			},
			"current": schema.StringAttribute{
				Description: "Current migration version",
				Computed:    true,
			},
			"next": schema.StringAttribute{
				Description: "Next migration version",
				Computed:    true,
			},
			"latest": schema.StringAttribute{
				Description: "The latest version of the migration is in the migration directory",
				Computed:    true,
			},
			"id": schema.StringAttribute{
				Description: "The ID of the migration",
				Computed:    true,
			},
		},
	}
}

// Read implements datasource.DataSource.
func (d *MigrationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data MigrationDataSourceModel
	// Read Terraform configuration data into the model
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
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
	w, cleanup, err := data.Workspace(ctx, &d.ProviderData)
	if err != nil {
		resp.Diagnostics.AddError("Generate config failure",
			fmt.Sprintf("Failed to create workspace: %s", err.Error()))
		return
	}
	defer cleanup()
	r, err := w.Exec.MigrateStatus(ctx, &atlas.MigrateStatusParams{
		Env:  w.Project.EnvName,
		Vars: w.Project.Vars,
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to read migration status", err.Error())
		return
	}
	switch u, err := url.Parse(r.Env.Dir); {
	case err != nil:
		resp.Diagnostics.AddError("Failed to parse migration directory URL", err.Error())
		return
	case u.Scheme == SchemaTypeAtlas:
		data.RemoteDir = &RemoteDirBlock{
			Name: types.StringValue(path.Join(u.Host, u.Path)),
			Tag:  types.StringNull(),
		}
		if t := u.Query().Get("tag"); t != "" {
			data.RemoteDir.Tag = types.StringValue(t)
		}
	}
	data.Status = types.StringValue(r.Status)
	if r.Status == "PENDING" && r.Current == noMigration {
		data.Current = types.StringValue("")
	} else {
		data.Current = types.StringValue(r.Current)
	}
	if r.Status == "OK" && r.Next == latestVersion {
		data.Next = types.StringValue("")
	} else {
		data.Next = types.StringValue(r.Next)
	}
	v := r.LatestVersion()
	if v == "" {
		data.Latest = types.StringNull()
	} else {
		data.Latest = types.StringValue(v)
	}
	if data.Latest.IsNull() {
		resp.Diagnostics.AddWarning("The migration directory is empty",
			"Please add migration files to the directory to start using migrations.")
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (d *MigrationDataSourceModel) Workspace(ctx context.Context, p *ProviderData) (*Workspace, func(), error) {
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
		},
	}
	m := migrationConfig{
		RevisionsSchema: d.RevisionsSchema.ValueString(),
		Repo:            repoConfig(d.Cloud, p.Cloud),
	}
	switch rd := d.RemoteDir; {
	case rd != nil:
		m.DirURL, err = rd.AtlasURL()
	case d.Config.ValueString() == "":
		// If no config is provided, use the default migrations directory.
		m.DirURL, err = absoluteFileURL(
			defaultString(d.DirURL, "migrations"))
	case d.DirURL.ValueString() != "":
		m.DirURL, err = absoluteFileURL(d.DirURL.ValueString())
	}
	if err != nil {
		return nil, nil, err
	}
	if m != (migrationConfig{}) {
		cfg.Env.Migration = &m
	}
	if vars := d.Vars.ValueString(); vars != "" {
		if err = json.Unmarshal([]byte(vars), &cfg.Vars); err != nil {
			return nil, nil, fmt.Errorf("failed to parse variables: %w", err)
		}
	}
	wd, err := atlas.NewWorkingDir(atlas.WithAtlasHCL(cfg.Render))
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

// AtlasURL returns the atlas URL for the remote directory.
func (r *RemoteDirBlock) AtlasURL() (string, error) {
	q := url.Values{}
	n := r.Name.ValueString()
	if n == "" {
		return "", fmt.Errorf("remote_dir.name is required")
	}
	if t := r.Tag.ValueString(); t != "" {
		q.Set("tag", t)
	}
	return (&url.URL{
		Scheme:   SchemaTypeAtlas,
		Path:     n,
		RawQuery: q.Encode(),
	}).String(), nil
}
