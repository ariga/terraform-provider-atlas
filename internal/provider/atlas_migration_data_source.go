package provider

import (
	"context"

	"ariga.io/ariga/terraform-provider-atlas/internal/atlas"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type (
	// MigrationDataSource defines the data source implementation.
	MigrationDataSource struct {
		providerData
	}
	// MigrationDataSourceModel describes the data source data model.
	MigrationDataSourceModel struct {
		DirURL          types.String `tfsdk:"dir"`
		URL             types.String `tfsdk:"url"`
		RevisionsSchema types.String `tfsdk:"revisions_schema"`

		Status  types.String `tfsdk:"status"`
		Current types.String `tfsdk:"current"`
		Next    types.String `tfsdk:"next"`
		Latest  types.String `tfsdk:"latest"`
		ID      types.String `tfsdk:"id"`
	}
)

// Ensure provider defined types fully satisfy framework interfaces
var (
	_ datasource.DataSource              = &MigrationDataSource{}
	_ datasource.DataSourceWithConfigure = &MigrationDataSource{}
)
var (
	latestVersion = "Already at latest version"
	noMigration   = "No migration applied yet"
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
func (d *MigrationDataSource) GetSchema(ctx context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: "Data source returns the information about the current migration.",
		Attributes: map[string]tfsdk.Attribute{
			"url": {
				Description: "[driver://username:password@address/dbname?param=value] select a resource using the URL format",
				Type:        types.StringType,
				Required:    true,
				Sensitive:   true,
			},
			"dir": {
				Description: "Select migration directory using URL format",
				Type:        types.StringType,
				Required:    true,
			},
			"revisions_schema": {
				Description: "The name of the schema the revisions table resides in",
				Type:        types.StringType,
				Optional:    true,
			},
			"status": {
				Description: "The Status of migration (OK, PENDING)",
				Type:        types.StringType,
				Computed:    true,
			},
			"current": {
				Description: "Current migration version",
				Type:        types.StringType,
				Computed:    true,
			},
			"next": {
				Description: "Next migration version",
				Type:        types.StringType,
				Computed:    true,
			},
			"latest": {
				Description: "The latest version of the migration is in the migration directory",
				Type:        types.StringType,
				Computed:    true,
			},
			"id": {
				Description: "The ID of the migration",
				Type:        types.StringType,
				Computed:    true,
			},
		},
	}, nil
}

// Read implements datasource.DataSource.
func (d *MigrationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data MigrationDataSourceModel
	// Read Terraform configuration data into the model
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r, err := d.client.Status(ctx, &atlas.StatusParams{
		DirURL:          data.DirURL.Value,
		URL:             data.URL.Value,
		RevisionsSchema: data.RevisionsSchema.Value,
	})
	if err != nil {
		resp.Diagnostics.Append(atlas.ErrorDiagnostic(err, "Failed to read migration status"))
		return
	}
	data.Status = types.String{Value: r.Status}
	if r.Status == "PENDING" && r.Current == noMigration {
		data.Current = types.String{Null: true}
	} else {
		data.Current = types.String{Value: r.Current}
	}
	if r.Status == "OK" && r.Next == latestVersion {
		data.Next = types.String{Null: true}
	} else {
		data.Next = types.String{Value: r.Next}
	}
	v := r.LatestVersion()
	data.ID = data.DirURL
	data.Latest = types.String{Value: v, Null: v == ""}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
