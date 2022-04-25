package atlas

import (
	"context"

	"ariga.io/atlas/sql"
	atlasSchema "ariga.io/atlas/sql/schema"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func newDatabaseSchema() *schema.Resource {
	return &schema.Resource{
		Description: "Atlas database resource manages the data schema of the database, using an HCL file describing the wanted state of the database. see https://atlasgo.io/",
		// Create&Update both apply migrations
		CreateContext: createSchema,
		UpdateContext: createSchema,
		ReadContext:   readSchema,
		DeleteContext: readSchema,
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
		},
	}
}

func readSchema(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	url := d.Get("url").(string)

	remoteHCL, err := sql.Inspect(ctx, url)
	if err != nil {
		return diag.FromErr(err)
	}

	d.Set("hcl", string(remoteHCL))
	d.Set("url", url)
	return diags
}

func createSchema(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	url := d.Get("url").(string)
	hcl := d.Get("hcl").(string)

	drv, err := sql.DefaultMux.OpenAtlas(ctx, url)
	if err != nil {
		return diag.FromErr(err)
	}

	realm, err := drv.InspectRealm(ctx, nil)
	if err != nil {
		return diag.FromErr(err)
	}

	desired := &atlasSchema.Realm{}
	err = drv.UnmarshalSpec([]byte(hcl), desired)
	if err != nil {
		return diag.FromErr(err)
	}

	desired, err = drv.Driver.(atlasSchema.Normalizer).NormalizeRealm(ctx, desired)
	if err != nil {
		return diag.FromErr(err)
	}

	changes, err := drv.RealmDiff(realm, desired)
	if err != nil {
		return diag.FromErr(err)
	}

	err = drv.ApplyChanges(ctx, changes)
	if err != nil {
		return diag.FromErr(err)
	}
	d.SetId(url)

	desiredHCL, err := drv.MarshalSpec(desired)
	if err != nil {
		return diag.FromErr(err)
	}
	d.Set("hcl", string(desiredHCL))
	return diags
}
