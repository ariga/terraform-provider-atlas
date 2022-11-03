package provider

import (
	"bytes"
	"context"
	"os"
	"path"

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
	"golang.org/x/mod/semver"

	"ariga.io/ariga/terraform-provider-atlas/internal/atlas"
	"ariga.io/ariga/terraform-provider-atlas/internal/vercheck"
)

type (
	// AtlasProvider defines the provider implementation.
	AtlasProvider struct {
		// client is the client used to interact with the Atlas CLI.
		client *atlas.Client
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
	_ provider.Provider                   = &AtlasProvider{}
	_ provider.ProviderWithMetadata       = &AtlasProvider{}
	_ provider.ProviderWithValidateConfig = &AtlasProvider{}
)

const (
	// envNoUpdate when enabled it cancels checking for update
	envNoUpdate = "ATLAS_NO_UPDATE_NOTIFIER"
	vercheckURL = "https://vercheck.ariga.io"
	versionFile = "release.json"
)

// New returns a new provider.
func New(version, commit string) func() provider.Provider {
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
	c, err := atlas.NewClient("atlas")
	if err != nil {
		resp.Diagnostics.AddError("Failed to create client", err.Error())
		return
	}
	p.client = c
	resp.DataSourceData = c
	resp.ResourceData = c
}

// DataSources implements provider.Provider.
func (p *AtlasProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewAtlasSchemaDataSource,
		NewMigrationDataSource,
	}
}

// Resources implements provider.Provider.
func (p *AtlasProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewAtlasSchemaResource,
		NewMigrationResource,
	}
}

// ConfigValidators returns a list of functions which will all be performed during validation.
func (p *AtlasProvider) ValidateConfig(ctx context.Context, req provider.ValidateConfigRequest, resp *provider.ValidateConfigResponse) {
	msg := checkForUpdate(ctx, p.version)()
	if msg != "" {
		resp.Diagnostics.AddWarning(
			"Update Available",
			msg,
		)
	}
}

func noText() string { return "" }

// checkForUpdate checks for version updates and security advisories for Atlas.
func checkForUpdate(ctx context.Context, version string) func() string {
	done := make(chan struct{})
	// Users may skip update checking behavior.
	if v := os.Getenv(envNoUpdate); v != "" {
		return noText
	}
	// Skip if the current binary version isn't set (dev mode).
	if !semver.IsValid(version) {
		return noText
	}
	curDir, err := os.Getwd()
	if err != nil {
		return noText
	}
	var message string
	go func() {
		defer close(done)
		vc := vercheck.New(vercheckURL, path.Join(curDir, versionFile))
		payload, err := vc.Check(version)
		if err != nil {
			return
		}
		var b bytes.Buffer
		if err := vercheck.Notify.Execute(&b, payload); err != nil {
			return
		}
		message = b.String()
	}()
	return func() string {
		select {
		case <-done:
		case <-ctx.Done():
		}
		return message
	}
}
