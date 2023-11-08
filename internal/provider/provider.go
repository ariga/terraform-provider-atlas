package provider

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"

	"github.com/mitchellh/go-homedir"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	tfpath "github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"golang.org/x/mod/semver"

	"ariga.io/ariga/terraform-provider-atlas/internal/vercheck"
	atlas "ariga.io/atlas-go-sdk/atlasexec"
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
		data    providerData
	}
	// AtlasProviderModel describes the provider data model.
	AtlasProviderModel struct {
		// DevURL is the URL of the dev-db.
		DevURL types.String `tfsdk:"dev_url"`
		// Cloud is the Atlas Cloud configuration.
		Cloud *AtlasCloudBlock `tfsdk:"cloud"`
	}
	AtlasCloudBlock struct {
		Token   types.String `tfsdk:"token"`
		URL     types.String `tfsdk:"url"`
		Project types.String `tfsdk:"project"`
	}
	providerData struct {
		// client is the client used to interact with the Atlas CLI.
		client *atlas.Client
		// devURL is the URL of the dev-db.
		devURL string
		// cloud is the Atlas Cloud configuration.
		cloud *AtlasCloudBlock
		// version is set to the provider version on release
		version string
	}
)

var (
	cloudBlock = schema.SingleNestedBlock{
		Attributes: map[string]schema.Attribute{
			"token": schema.StringAttribute{
				Optional: true,
			},
			"url": schema.StringAttribute{
				Optional: true,
			},
			"project": schema.StringAttribute{
				Optional: true,
			},
		},
	}
)

// Ensure AtlasProvider satisfies various provider interfaces.
var (
	_ provider.Provider                   = &AtlasProvider{}
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
func (p *AtlasProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The Atlas provider is used to manage your database migrations, using the DDL of Atlas.\n" +
			"For documentation about Atlas, visit: https://atlasgo.io",
		Blocks: map[string]schema.Block{
			"cloud": cloudBlock,
		},
		Attributes: map[string]schema.Attribute{
			"dev_url": schema.StringAttribute{
				Description: "The URL of the dev database. This configuration is shared for all resources if there is no config on the resource.",
				Optional:    true,
				Sensitive:   true,
			},
		},
	}
}

// Configure implements provider.Provider.
func (p *AtlasProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	atlasPath, err := execPath(p.dir, "atlas")
	if err != nil {
		resp.Diagnostics.AddError("Unable to find atlas-cli", err.Error())
		return
	}
	c, err := atlas.NewClient("", atlasPath)
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
	p.data = providerData{client: c, cloud: model.Cloud, version: p.version}
	if model != nil {
		p.data.devURL = model.DevURL.ValueString()
	}
	resp.DataSourceData = p.data
	resp.ResourceData = p.data
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
		if s := u.ValueString(); s != "" {
			return s
		}
	}
	return d.devURL
}

func (d *providerData) configure(data any) (diags diag.Diagnostics) {
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
	if d.client == nil {
		// TF run validation on resource/data-source before configure,
		// so we can't validate the config at this point.
		// If the client is nil, it means that the provider has not been configured.
		return
	}
	var devURL types.String
	diags.Append(cfg.GetAttribute(ctx, tfpath.Root("dev_url"), &devURL)...)
	if diags.HasError() {
		return diags
	}
	if !devURL.IsUnknown() && devURL.ValueString() == "" && d.devURL == "" {
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

func execPath(dir, name string) (file string, err error) {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	file = filepath.Join(dir, name)
	if _, err = os.Stat(file); err == nil {
		return file, nil
	}
	// If the binary is not in the current directory,
	// try to find it in the PATH.
	return exec.LookPath(name)
}
