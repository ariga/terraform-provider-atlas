package provider

import (
	"bytes"
	"context"
	"fmt"
	"os"

	"github.com/mitchellh/go-homedir"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	tfpath "github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"golang.org/x/mod/semver"

	"ariga.io/ariga/terraform-provider-atlas/internal/vercheck"
	atlas "ariga.io/atlas-go-sdk/atlasexec"
)

type (
	// AtlasProvider defines the provider implementation.
	AtlasProvider struct {
		data ProviderData
	}
	// AtlasProviderModel describes the provider data model.
	AtlasProviderModel struct {
		// BinaryPath is the path to the atlas-cli binary.
		BinaryPath types.String `tfsdk:"binary_path"`
		// DevURL is the URL of the dev-db.
		DevURL types.String `tfsdk:"dev_url"`
		// Cloud is the Atlas Cloud configuration.
		Cloud *AtlasCloudBlock `tfsdk:"cloud"`
	}
	AtlasCloudBlock struct {
		Token   types.String `tfsdk:"token"`
		URL     types.String `tfsdk:"url"`
		Project types.String `tfsdk:"project"`
		Repo    types.String `tfsdk:"repo"`
	}
	AtlasExec interface {
		MigrateApply(context.Context, *atlas.MigrateApplyParams) (*atlas.MigrateApply, error)
		MigrateDown(context.Context, *atlas.MigrateDownParams) (*atlas.MigrateDown, error)
		MigrateLint(context.Context, *atlas.MigrateLintParams) (*atlas.SummaryReport, error)
		MigrateStatus(context.Context, *atlas.MigrateStatusParams) (*atlas.MigrateStatus, error)

		SchemaInspect(context.Context, *atlas.SchemaInspectParams) (string, error)
		SchemaApply(context.Context, *atlas.SchemaApplyParams) (*atlas.SchemaApply, error)
		SchemaClean(context.Context, *atlas.SchemaCleanParams) (*atlas.SchemaClean, error)
		SchemaPlan(context.Context, *atlas.SchemaPlanParams) (*atlas.SchemaPlan, error)
		SchemaPlanList(context.Context, *atlas.SchemaPlanListParams) ([]atlas.SchemaPlanFile, error)
		SchemaPush(context.Context, *atlas.SchemaPushParams) (*atlas.SchemaPush, error)
		WhoAmI(context.Context, *atlas.WhoAmIParams) (*atlas.WhoAmI, error)

		Version(context.Context) (*atlas.Version, error)

		Login(context.Context, *atlas.LoginParams) error
	}
	ProviderData struct {
		// DevURL is the URL of the dev-db.
		DevURL string
		// Cloud is the Atlas Cloud configuration.
		Cloud *AtlasCloudBlock
		// Client is the factory function to create a new AtlasExec Client.
		// It is set during the provider configuration.
		Client func(wd string, c *CloudConfig) (AtlasExec, error)
		// version is set to the provider version on release, "dev" when the
		// provider is built and ran locally, and "test" when running acceptance
		// testing.
		Version string
	}
	// Workspace represents the working directory and client for the resource.
	Workspace struct {
		Project *projectConfig
		Dir     *atlas.WorkingDir
		Exec    AtlasExec
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
				Optional:           true,
				DeprecationMessage: "Use the repo attribute instead. The project attribute will be removed in a future version.",
			},
			"repo": schema.StringAttribute{
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
	return func() provider.Provider {
		return &AtlasProvider{data: ProviderData{Version: version}}
	}
}

// Metadata implements provider.ProviderWithMetadata.
func (p *AtlasProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "atlas"
	resp.Version = p.data.Version
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
			"binary_path": schema.StringAttribute{
				Description: "The path to the atlas-cli binary. If not set, the provider will look for the binary in the PATH.",
				Optional:    true,
			},
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
	var model *AtlasProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	binPath := "atlas"
	if s := model.BinaryPath.ValueString(); s != "" {
		binPath = s
	}
	fnClient := func(wd string, cloud *CloudConfig) (AtlasExec, error) {
		c, err := atlas.NewClient(wd, binPath)
		if err != nil {
			return nil, err
		}
		env := atlas.NewOSEnviron()
		env["ATLAS_INTEGRATION"] = fmt.Sprintf("terraform-provider-atlas/v%s", p.data.Version)
		if cloud != nil && cloud.Token != "" {
			env["ATLAS_TOKEN"] = cloud.Token
		}
		if err = c.SetEnv(env); err != nil {
			return nil, err
		}
		return c, nil
	}
	var cloud *CloudConfig
	if model != nil && model.Cloud.Valid() {
		cloud = &CloudConfig{
			Token: model.Cloud.Token.ValueString(),
		}
	}
	c, err := fnClient("", cloud)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create client", err.Error())
		return
	}
	v, err := c.Version(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Check atlas version failure", err.Error())
		return
	}
	version := fmt.Sprintf("%s-%s", v.Version, v.SHA)
	if v.Canary {
		version += "-canary"
	}
	tflog.Debug(ctx, "found atlas-cli", map[string]any{"version": version})
	p.data.Client = fnClient
	p.data.Cloud = model.Cloud
	if model != nil {
		p.data.DevURL = model.DevURL.ValueString()
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
	v := p.data.Version
	if v == "dev" || v == "test" {
		return
	}
	msg, err := checkForUpdate(ctx, fmt.Sprintf("v%s", p.data.Version))
	if err != nil {
		tflog.Error(ctx, "failed to check for update", map[string]interface{}{
			"error": err,
		})
		return
	}
	if msg != "" {
		resp.Diagnostics.AddWarning("Update Available", msg)
	}
}

func (d *ProviderData) configure(data any) (diags diag.Diagnostics) {
	// Prevent panic if the provider has not been configured.
	if data == nil {
		return
	}
	if parent, ok := data.(ProviderData); ok {
		// Copy the parent data to the receiver.
		*d = parent
	} else {
		diags.AddError("Unexpected Configure Type",
			fmt.Sprintf("Expected ProviderData, got: %T. Please report this issue to the provider developers.", data),
		)
	}
	return diags
}

type atlasWorkspace interface {
	Workspace(context.Context, *ProviderData) (*Workspace, func(), error)
}

func (d *ProviderData) validate(ctx context.Context, data atlasWorkspace) (diags diag.Diagnostics) {
	if d.Client == nil {
		// TF run validation on resource/data-source before configure,
		// so we can't validate the config at this point.
		// If the client is nil, it means that the provider has not been configured.
		return
	}
	w, cleanup, err := data.Workspace(ctx, d)
	if err != nil {
		diags.AddError("Generate config failure",
			fmt.Sprintf("Failed to create workspace: %s", err.Error()))
		return
	}
	defer cleanup()
	// The atlas.hcl file is required to be present in the working directory.
	raw, err := os.ReadFile(w.Dir.Path("atlas.hcl"))
	if err != nil {
		diags.AddError("Read atlas.hcl failure", err.Error())
		return
	}
	tflog.Debug(ctx, "atlas.hcl", map[string]any{"content": string(raw)})
	f, wdiags := hclwrite.ParseConfig(raw, "atlas.hcl", hcl.InitialPos)
	if wdiags.HasErrors() {
		return
	}
	// Check if the env block exists.
	blk, err := searchBlock(f.Body(), "env", w.Project.EnvName)
	switch {
	case err != nil:
		diags.AddError("Invalid atlas.hcl config", fmt.Sprintf(`
%s

the atlas.hcl content:

%s
`, err.Error(), string(raw)))
		return
	case blk == nil:
		diags.AddError("Invalid atlas.hcl config", "No env blocks found")
		return
	default:
		// Just check if the dev_url is set.
		// We can't check the attributes' value because it require EvalContext.
		if _, found := blk.Body().Attributes()["dev"]; !found {
			diags.AddAttributeWarning(tfpath.Root("dev_url"), "dev_url is unset",
				"It is highly recommended that you use 'dev_url' to specify a dev database.\n"+
					"to learn more about it, visit: https://atlasgo.io/dev-database")
		}
	}
	return diags
}

// Valid returns true if the cloud block is valid.
func (c *AtlasCloudBlock) Valid() bool {
	return c != nil && (c.Token.ValueString() != "" || c.Repo.ValueString() != "")
}

func cloudConfig(c ...*AtlasCloudBlock) *CloudConfig {
	var cloud CloudConfig
	for _, b := range c {
		if b.Valid() && cloud.Token == "" {
			cloud.Token = b.Token.ValueString()
		}
	}
	return &cloud
}

func repoConfig(c ...*AtlasCloudBlock) string {
	repo := ""
	for _, b := range c {
		if b.Valid() && repo == "" {
			// Backward compatibility with the project attribute.
			if b.Repo.ValueString() == "" {
				repo = b.Project.ValueString()
				continue
			}
			repo = b.Repo.ValueString()
		}
	}
	return repo
}

// checkForUpdate checks for version updates and security advisories for Atlas.
func checkForUpdate(_ context.Context, version string) (string, error) {
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
