package provider

import (
	"context"

	"ariga.io/ariga/terraform-provider-atlas/internal/atlas"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
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
func (d *MigrationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Data source returns the information about the current migration.",
		Attributes: map[string]schema.Attribute{
			"url": schema.StringAttribute{
				Description: "[driver://username:password@address/dbname?param=value] select a resource using the URL format",
				Required:    true,
				Sensitive:   true,
			},
			"dir": schema.StringAttribute{
				Description: "Select migration directory using URL format",
				Required:    true,
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
	r, err := d.client.Status(ctx, &atlas.StatusParams{
		DirURL:          data.DirURL.ValueString(),
		URL:             data.URL.ValueString(),
		RevisionsSchema: data.RevisionsSchema.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.Append(atlas.ErrorDiagnostic(err, "Failed to read migration status"))
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
	data.ID = data.DirURL
	if v == "" {
		data.Latest = types.StringNull()
	} else {
		data.Latest = types.StringValue(v)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
