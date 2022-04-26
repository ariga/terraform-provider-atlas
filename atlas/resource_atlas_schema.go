package atlas

import (
	"context"

	"ariga.io/atlas/sql"
	atlaschema "ariga.io/atlas/sql/schema"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func newSchemaResource() *schema.Resource {
	return &schema.Resource{
		// Create&Update both apply migrations
		CreateContext: applySchema,
		UpdateContext: applySchema,
		ReadContext:   readSchema,
		DeleteContext: readSchema,
		Schema: map[string]*schema.Schema{
			"hcl": {
				Type:        schema.TypeString,
				Description: "The schema definition for the database",
				Required:    true,
			},
			"url": {
				Type:        schema.TypeString,
				Description: "A connection url for the database",
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

func applySchema(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
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

	desired := &atlaschema.Realm{}
	if err = drv.UnmarshalSpec([]byte(hcl), desired); err != nil {
		return diag.FromErr(err)
	}

	desired, err = drv.Driver.(atlaschema.Normalizer).NormalizeRealm(ctx, desired)
	if err != nil {
		return diag.FromErr(err)
	}
	changes, err := drv.RealmDiff(realm, desired)
	if err != nil {
		return diag.FromErr(err)
	}
	if err = drv.ApplyChanges(ctx, changes); err != nil {
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
