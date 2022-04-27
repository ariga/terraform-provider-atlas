package atlas

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"ariga.io/atlas/sql"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

const testAccActionConfigCreate = `
data "atlas_schema" "at_schema" {
  dev_db_url = "mysql://root:pass@tcp(localhost:3307)/test"
  src = <<-EOT
	schema "test" {
		charset = "latin1"
		collate = "latin1_swedish_ci"
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
  hcl = data.atlas_schema.at_schema.hcl
  url = "mysql://root:pass@tcp(localhost:3306)/test"
}
`

const testAccActionConfigUpdate = `
data "atlas_schema" "at_schema" {
  dev_db_url = "mysql://root:pass@tcp(localhost:3307)/test"
  src = <<-EOT
	schema "test" {
		charset = "latin1"
		collate = "latin1_swedish_ci"
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
  hcl = data.atlas_schema.at_schema.hcl
  url = "mysql://root:pass@tcp(localhost:3306)/test"
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
					resource.TestCheckResourceAttr("atlas_schema.testdb", "id", "mysql://root:pass@tcp(localhost:3306)/test"),
				),
			},
			{
				Config: testAccActionConfigUpdate,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("atlas_schema.testdb", "id", "mysql://root:pass@tcp(localhost:3306)/test"),
					func(s *terraform.State) error {
						res := s.RootModule().Resources["atlas_schema.testdb"]
						hcl, err := sql.Inspect(context.TODO(), res.Primary.ID, "test")
						if err != nil {
							return err
						}
						if !strings.Contains(string(hcl), `column "name"`) {
							return fmt.Errorf("expected database state to contain column \"name\" but it didn't\n:%s", string(hcl))
						}
						return nil
					},
				),
			},
		},
	})
}
