package provider

import (
	"context"

	_ "ariga.io/atlas/sql/mysql"
	_ "ariga.io/atlas/sql/postgres"
	_ "ariga.io/atlas/sql/sqlite"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
)

type (
	// AtlasProvider defines the provider implementation.
	AtlasProvider struct {
		// version is set to the provider version on release, "dev" when the
		// provider is built and ran locally, and "test" when running acceptance
		// testing.
		version string
	}
	// AtlasProviderModel describes the provider data model.
	AtlasProviderModel struct{}
)

// Ensure AtlasProvider satisfies various provider interfaces.
var (
	_ provider.Provider             = &AtlasProvider{}
	_ provider.ProviderWithMetadata = &AtlasProvider{}
)

// New returns a new provider.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &AtlasProvider{
			version: version,
		}
	}
}

// Metadata implements provider.ProviderWithMetadata.
func (p *AtlasProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "atlas"
	resp.Version = p.version
}

// GetSchema implements provider.Provider.
func (p *AtlasProvider) GetSchema(ctx context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: "The Atlas provider is used to manage your database migrations, using the DDL of Atlas.\n" +
			"For documentation about Atlas, visit: https://atlasgo.io",
		Attributes: map[string]tfsdk.Attribute{},
	}, nil
}

// Configure implements provider.Provider.
func (p *AtlasProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
}

// DataSources implements provider.Provider.
func (p *AtlasProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewAtlasSchemaDataSource,
	}
}

// Resources implements provider.Provider.
func (p *AtlasProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewAtlasSchemaResource,
	}
}
