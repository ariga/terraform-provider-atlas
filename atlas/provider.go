package atlas

import (
	_ "ariga.io/atlas/sql/mysql"
	_ "ariga.io/atlas/sql/postgres"
	_ "ariga.io/atlas/sql/sqlite"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

func Provider() *schema.Provider {
	return &schema.Provider{
		Schema: map[string]*schema.Schema{},
		ResourcesMap: map[string]*schema.Resource{
			"atlas_schema": newSchemaResource(),
		},
		DataSourcesMap: map[string]*schema.Resource{
			"atlas_schema": newSchemaDatasource(),
		},
	}
}
