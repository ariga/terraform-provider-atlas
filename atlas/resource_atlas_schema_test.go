package atlas

import (
	"context"
	"fmt"
	"testing"

	"ariga.io/atlas/sql/sqlclient"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

const testAccActionConfigCreate = `
data "atlas_schema" "market" {
  dev_db_url = "mysql://root:pass@localhost:3307/test"
  src = <<-EOT
	schema "test" {
		charset = "utf8mb4"
		collate = "utf8mb4_0900_ai_ci"
	}
	table "foo" {
		schema = schema.test
		column "id" {
			null           = false
			type           = int
			auto_increment = true
		}
		primary_key {
			columns = [column.id]
		}
	}
	EOT
}
resource "atlas_schema" "testdb" {
  hcl = data.atlas_schema.market.hcl
  url = "mysql://root:pass@localhost:3306/test"
}
`

const testAccActionConfigUpdate = `
data "atlas_schema" "market" {
  dev_db_url = "mysql://root:pass@localhost:3307/test"
  src = <<-EOT
	schema "test" {
		charset = "utf8mb4"
		collate = "utf8mb4_0900_ai_ci"
	}
	table "foo" {
		schema = schema.test
		column "id" {
			null           = false
			type           = int
			auto_increment = true
		}
		column "name" {
			null = false
			type = varchar(20)
		}
		primary_key {
			columns = [column.id]
		}
	}
	EOT
}
resource "atlas_schema" "testdb" {
  hcl = data.atlas_schema.market.hcl
  url = "mysql://root:pass@localhost:3306/test"
}
`

func TestAccAtlasDatabase(t *testing.T) {
	resource.Test(t, resource.TestCase{
		Providers: map[string]*schema.Provider{
			"atlas": Provider(),
		},
		Steps: []resource.TestStep{
			{
				Config: testAccActionConfigCreate,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("atlas_schema.testdb", "id", "mysql://root:pass@localhost:3306/test"),
				),
			},
			{
				Config: testAccActionConfigUpdate,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("atlas_schema.testdb", "id", "mysql://root:pass@localhost:3306/test"),
					func(s *terraform.State) error {
						res := s.RootModule().Resources["atlas_schema.testdb"]
						cli, err := sqlclient.Open(context.TODO(), res.Primary.ID)
						if err != nil {
							return err
						}
						realm, err := cli.InspectRealm(context.TODO(), nil)
						if err != nil {
							return err
						}

						if realm.Schemas[0].Tables[0].Columns[1].Name != "name" {
							return fmt.Errorf("expected database state to have column \"name\" but got: %s", realm.Schemas[0].Tables[0].Columns[1].Name)
						}
						return nil
					},
				),
			},
		},
	})
}
