package provider

import (
	"context"
	"fmt"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	atlas "ariga.io/atlas-go-sdk/atlasexec"
)

type (
	// MigrationDataSource defines the data source implementation.
	MigrationDataSource struct {
		providerData
	}
	// MigrationDataSourceModel describes the data source data model.
	MigrationDataSourceModel struct {
		URL             types.String `tfsdk:"url"`
		RevisionsSchema types.String `tfsdk:"revisions_schema"`

		DirURL    types.String     `tfsdk:"dir"`
		Cloud     *AtlasCloudBlock `tfsdk:"cloud"`
		RemoteDir *RemoteDirBlock  `tfsdk:"remote_dir"`

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
	_ datasource.DataSource              = &MigrationDataSource{}
	_ datasource.DataSourceWithConfigure = &MigrationDataSource{}
)
var (
	latestVersion  = "Already at latest version"
	noMigration    = "No migration applied yet"
	remoteDirBlock = schema.SingleNestedBlock{
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

// GetSchema implements datasource.DataSource.
func (d *MigrationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Data source returns the information about the current migration.",
		Blocks: map[string]schema.Block{
			"cloud":      cloudBlock,
			"remote_dir": remoteDirBlock,
		},
		Attributes: map[string]schema.Attribute{
			"url": schema.StringAttribute{
				Description: "[driver://username:password@address/dbname?param=value] select a resource using the URL format",
				Required:    true,
				Sensitive:   true,
			},
			"dir": schema.StringAttribute{
				Description: "Select migration directory using URL format",
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
	cfg, err := data.projectConfig(d.cloud)
	if err != nil {
		resp.Diagnostics.AddError("Generate config failure", err.Error())
		return
	}
	wd, err := atlas.NewWorkingDir(atlas.WithAtlasHCL(cfg.Render))
	if err != nil {
		resp.Diagnostics.AddError("Generate config failure",
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
	c, err := d.client(wd.Path())
	if err != nil {
		resp.Diagnostics.AddError("Failed to create client", err.Error())
		return
	}
	r, err := c.MigrateStatus(ctx, &atlas.MigrateStatusParams{
		Env: cfg.EnvName,
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to read migration status", err.Error())
		return
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
	data.ID = dirToID(data.RemoteDir, data.DirURL)
	if v == "" {
		data.Latest = types.StringNull()
	} else {
		data.Latest = types.StringValue(v)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (d *MigrationDataSourceModel) projectConfig(cloud *AtlasCloudBlock) (*projectConfig, error) {
	dbURL, err := absoluteSqliteURL(d.URL.ValueString())
	if err != nil {
		return nil, err
	}
	cfg := projectConfig{
		Config:  baseAtlasHCL,
		EnvName: "tf",
		Env: &envConfig{
			URL: dbURL,
			Migration: &migrationConfig{
				RevisionsSchema: d.RevisionsSchema.ValueString(),
			},
		},
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
