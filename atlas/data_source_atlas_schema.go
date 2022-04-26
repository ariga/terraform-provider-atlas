package atlas

import (
	"context"
	"encoding/base64"
	"hash/fnv"

	"ariga.io/atlas/sql"
	atlaschema "ariga.io/atlas/sql/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func newSchemaDatasource() *schema.Resource {
	return &schema.Resource{
		Description: "atlas_schema data source uses dev-db to normalize the HCL schema in order to create better terraform diffs",
		ReadContext: normalize,
		Schema: map[string]*schema.Schema{
			"dev_db_url": {
				Description: "The url of the dev-db see https://atlasgo.io/cli/url",
				Type:        schema.TypeString,
				Required:    true,
				Sensitive:   true,
			},
			"hcl": {
				Description: "The schema definition of the database",
				Type:        schema.TypeString,
				Required:    true,
			},
			// the HCL in a predicted, and ordered format see https://atlasgo.io/cli/dev-database
			"normal_hcl": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The normalized form of the HCL",
			},
		},
	}
}

func normalize(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	hcl := d.Get("hcl").(string)
	url := d.Get("dev_db_url").(string)

	drv, err := sql.DefaultMux.OpenAtlas(ctx, url)
	if err != nil {
		return diag.FromErr(err)
	}
	realm := &atlaschema.Realm{}
	if err = drv.UnmarshalSpec([]byte(hcl), realm); err != nil {
		return diag.FromErr(err)
	}
	realm, err = drv.Driver.(atlaschema.Normalizer).NormalizeRealm(ctx, realm)
	if err != nil {
		return diag.FromErr(err)
	}
	normalHCL, err := drv.MarshalSpec(realm)
	if err != nil {
		return diag.FromErr(err)
	}

	d.Set("normal_hcl", string(normalHCL))
	d.SetId(hclID(string(normalHCL)))
	return diag.Diagnostics{}
}

func hclID(hcl string) string {
	hasher := fnv.New128()
	hasher.Write([]byte(hcl))
	hash := hasher.Sum(nil)
	return base64.RawStdEncoding.EncodeToString(hash)
}
