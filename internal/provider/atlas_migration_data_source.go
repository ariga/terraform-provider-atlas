package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

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
	dir, err := os.MkdirTemp(os.TempDir(), "tf-atlas-*")
	if err != nil {
		resp.Diagnostics.AddError("Generate config failure",
			fmt.Sprintf("Failed to create temporary directory: %s", err.Error()))
		return
	}
	defer os.RemoveAll(dir)
	cfgPath := filepath.Join(dir, "atlas.hcl")
	if err := data.AtlasHCL(cfgPath, d.cloud); err != nil {
		resp.Diagnostics.AddError("Generate config failure",
			fmt.Sprintf("Failed to write configuration file: %s", err.Error()))
		return
	}
	r, err := d.client.MigrateStatus(ctx, &atlas.MigrateStatusParams{
		ConfigURL: fmt.Sprintf("file://%s", filepath.ToSlash(cfgPath)),
		Env:       "tf",
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

func (d *MigrationDataSourceModel) AtlasHCL(path string, cloud *AtlasCloudBlock) error {
	cfg := atlasHCL{
		URL: d.URL.ValueString(),
		Migration: &migrationConfig{
			RevisionsSchema: d.RevisionsSchema.ValueString(),
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
	return cfg.CreateFile(path)
}
