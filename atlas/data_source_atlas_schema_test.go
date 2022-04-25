package atlas

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

const testAccData = `
data "atlas_schema" "at_schema" {
  dev_db_url = "mysql://root:pass@tcp(localhost:3307)/test"
  hcl = <<-EOT
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
`

const normalHCL = `table "foo" {
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
schema "test" {
  charset = "latin1"
  collate = "latin1_swedish_ci"
}
`

func TestAccDataNormalHCL(t *testing.T) {
	resource.Test(t, resource.TestCase{
		Providers: map[string]*schema.Provider{
			"atlas": Provider(),
		},
		PreventPostDestroyRefresh: true,
		Steps: []resource.TestStep{
			{
				Config: testAccData,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.atlas_schema.at_schema", "normal_hcl", normalHCL),
				),
			},
		},
	})
}
