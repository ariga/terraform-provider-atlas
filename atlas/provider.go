package atlas

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func Provider() *schema.Provider {
	return &schema.Provider{
		Schema: map[string]*schema.Schema{},
		ResourcesMap: map[string]*schema.Resource{
			"atlas_schema": newDatabase(),
		},
		DataSourcesMap: map[string]*schema.Resource{
			"atlas_schema": newNormalizeDatasource(),
		},
	}
}
