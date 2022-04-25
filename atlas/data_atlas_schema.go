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

func newNormalizeDatasource() *schema.Resource {
	return &schema.Resource{
		ReadContext: readDataClient,
		Schema: map[string]*schema.Schema{
			"dev_db_url": {
				Type:     schema.TypeString,
				Required: true,
			},
			"hcl": {
				Type:     schema.TypeString,
				Required: true,
			},
			// the HCL in a predicted, and ordered format see https://atlasgo.io/cli/dev-database
			"content": {
				Type:     schema.TypeString,
				Computed: true,
			},
		},
	}
}

func readDataClient(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	hcl := d.Get("hcl").(string)
	url := d.Get("dev_db_url").(string)

	drv, err := sql.DefaultMux.OpenAtlas(ctx, url)
	if err != nil {
		return diag.FromErr(err)
	}
	realm := &atlaschema.Realm{}
	err = drv.UnmarshalSpec([]byte(hcl), realm)
	if err != nil {
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

	d.Set("content", string(normalHCL))
	d.SetId(hclID(string(normalHCL)))
	return diag.Diagnostics{}
}

func hclID(hcl string) string {
	hasher := fnv.New128()
	hasher.Write([]byte(hcl))
	hash := hasher.Sum(nil)
	return base64.RawStdEncoding.EncodeToString(hash)
}
