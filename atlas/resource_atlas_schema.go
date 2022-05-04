package atlas

import (
	"context"

	atlaschema "ariga.io/atlas/sql/schema"
	"ariga.io/atlas/sql/sqlclient"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func newSchemaResource() *schema.Resource {
	return &schema.Resource{
		Description: "Atlas database resource manages the data schema of the database, using an HCL file describing the wanted state of the database. see https://atlasgo.io/",
		// Create&Update both apply migrations
		CreateContext: applySchema,
		UpdateContext: applySchema,
		ReadContext:   readSchema,
		DeleteContext: deleteSchema,
		Schema: map[string]*schema.Schema{
			"hcl": {
				Type:        schema.TypeString,
				Description: "The schema definition for the database (preferably normalized - see `atlas_schema` data source)",
				Required:    true,
			},
			"url": {
				Type:        schema.TypeString,
				Description: "The url of the database see https://atlasgo.io/cli/url",
				Required:    true,
				Sensitive:   true,
			},
			"dev_db_url": {
				Description: "The url of the dev-db see https://atlasgo.io/cli/url",
				Type:        schema.TypeString,
				Optional:    true,
				Sensitive:   true,
			},
		},
	}
}

func deleteSchema(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	return nil
}

func readSchema(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	url := d.Get("url").(string)

	cli, err := sqlclient.Open(ctx, url)
	if err != nil {
		return diag.FromErr(err)
	}
	realm, err := cli.InspectRealm(ctx, nil)
	if err != nil {
		return diag.FromErr(err)
	}
	hcl, err := cli.MarshalSpec(realm)
	if err != nil {
		return diag.FromErr(err)
	}

	d.Set("hcl", string(hcl))
	d.Set("url", url)
	return diags
}

func applySchema(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	url := d.Get("url").(string)
	hcl := d.Get("hcl").(string)

	cli, err := sqlclient.Open(ctx, url)
	if err != nil {
		return diag.FromErr(err)
	}

	realm, err := cli.InspectRealm(ctx, nil)
	if err != nil {
		return diag.FromErr(err)
	}

	desired := &atlaschema.Realm{}
	if err = cli.Evaluator.Eval([]byte(hcl), desired, nil); err != nil {
		return diag.FromErr(err)
	}

	if devURL, ok := d.GetOk("dev_db_url"); ok {
		dev, err := sqlclient.Open(ctx, devURL.(string))
		if err != nil {
			return diag.FromErr(err)
		}
		defer dev.Close()
		desired, err = dev.Driver.(atlaschema.Normalizer).NormalizeRealm(ctx, desired)
		if err != nil {
			return diag.FromErr(err)
		}
	} else {
		diags = append(diags, diag.Diagnostic{
			Severity:      diag.Warning,
			AttributePath: cty.GetAttrPath("dev_db_url"),
			Summary:       "it is highly recommended that you use 'dev_db_url' to specify a dev database.\nto learn more about it, visit: https://atlasgo.io/dev-database",
		})
	}

	changes, err := cli.RealmDiff(realm, desired)
	if err != nil {
		return diag.FromErr(err)
	}
	if err = cli.ApplyChanges(ctx, changes); err != nil {
		return diag.FromErr(err)
	}
	d.SetId(url)

	desiredHCL, err := cli.MarshalSpec(desired)
	if err != nil {
		return diag.FromErr(err)
	}
	d.Set("hcl", string(desiredHCL))
	return diags
}
