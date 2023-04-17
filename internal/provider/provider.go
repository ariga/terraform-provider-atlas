package provider

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path"
	"runtime"

	_ "ariga.io/atlas/sql/mysql"
	_ "ariga.io/atlas/sql/postgres"
	_ "ariga.io/atlas/sql/sqlite"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
	"github.com/mitchellh/go-homedir"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	tfpath "github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"golang.org/x/mod/semver"

	"ariga.io/ariga/terraform-provider-atlas/internal/atlas"
	"ariga.io/ariga/terraform-provider-atlas/internal/vercheck"
)

type (
	// AtlasProvider defines the provider implementation.
	AtlasProvider struct {
		// client is the client used to interact with the Atlas CLI.
		client *atlas.Client
		// dir is the directory where the provider is installed.
		dir string
		// version is set to the provider version on release, "dev" when the
		// provider is built and ran locally, and "test" when running acceptance
		// testing.
		version string
	}
	// AtlasProviderModel describes the provider data model.
	AtlasProviderModel struct {
		// DevURL is the URL of the dev-db.
		DevURL types.String `tfsdk:"dev_url"`
	}
	providerData struct {
		// client is the client used to interact with the Atlas CLI.
		client *atlas.Client
		// devURL is the URL of the dev-db.
		devURL string
	}
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
	versionFile = "~/.atlas/terraform-provider-atlas-release.json"
)

// New returns a new provider.
func New(address, version, commit string) func() provider.Provider {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	providersDir := path.Join(wd, ".terraform", "providers")
	platform := fmt.Sprintf("%s_%s", runtime.GOOS, runtime.GOARCH)
	return func() provider.Provider {
		return &AtlasProvider{
			dir:     path.Join(providersDir, address, version, platform),
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
		Attributes: map[string]tfsdk.Attribute{
			"dev_url": {
				Description: "The URL of the dev database. This configuration is shared for all resources if there is no config on the resource.",
				Type:        types.StringType,
				Optional:    true,
				Sensitive:   true,
			},
		},
	}, nil
}

// Configure implements provider.Provider.
func (p *AtlasProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	c, err := atlas.NewClient(ctx, p.dir, "atlas")
	if err != nil {
		resp.Diagnostics.AddError("Failed to create client", err.Error())
		return
	}
	p.client = c

	var model *AtlasProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	data := providerData{client: c}
	if model != nil {
		data.devURL = model.DevURL.Value
	}
	resp.DataSourceData = data
	resp.ResourceData = data
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
	if p.version == "dev" || p.version == "test" {
		return
	}
	msg, err := checkForUpdate(ctx, fmt.Sprintf("v%s", p.version))
	if err != nil {
		tflog.Error(ctx, "failed to check for update", map[string]interface{}{
			"error": err,
		})
		return
	}
	if msg != "" {
		resp.Diagnostics.AddWarning(
			"Update Available",
			msg,
		)
	}
}

func (d *providerData) getDevURL(urls ...types.String) string {
	for _, u := range urls {
		if u.Value != "" {
			return u.Value
		}
	}
	return d.devURL
}

func (d *providerData) childrenConfigure(data any) (diags diag.Diagnostics) {
	// Prevent panic if the provider has not been configured.
	if data == nil {
		return
	}
	c, ok := data.(providerData)
	if !ok {
		diags.AddError("Unexpected Configure Type",
			fmt.Sprintf("Expected ProviderData, got: %T. Please report this issue to the provider developers.", data),
		)
		return
	}
	*d = c
	return diags
}

func (d *providerData) validateConfig(ctx context.Context, cfg tfsdk.Config) (diags diag.Diagnostics) {
	var devURL types.String
	diags.Append(cfg.GetAttribute(ctx, tfpath.Root("dev_url"), &devURL)...)
	if diags.HasError() {
		return diags
	}
	if !devURL.IsUnknown() && devURL.Value == "" && d.devURL == "" {
		diags.AddAttributeWarning(tfpath.Root("dev_url"), "dev_url is unset",
			"It is highly recommended that you use 'dev_url' to specify a dev database.\n"+
				"to learn more about it, visit: https://atlasgo.io/dev-database")
	}
	return diags
}

// checkForUpdate checks for version updates and security advisories for Atlas.
func checkForUpdate(ctx context.Context, version string) (string, error) {
	// Users may skip update checking behavior.
	if v := os.Getenv(envNoUpdate); v != "" {
		return "", nil
	}
	// Skip if the current binary version isn't set (dev mode).
	if !semver.IsValid(version) {
		return "", nil
	}
	path, err := homedir.Expand(versionFile)
	if err != nil {
		return "", err
	}
	vc := vercheck.New(vercheckURL, path)
	payload, err := vc.Check(version)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := vercheck.Notify.Execute(&b, payload); err != nil {
		return "", err
	}
	return b.String(), nil
}
