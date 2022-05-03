package atlas

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

const testAccData = `
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
  charset = "utf8mb4"
  collate = "utf8mb4_0900_ai_ci"
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
					resource.TestCheckResourceAttr("data.atlas_schema.market", "hcl", normalHCL),
				),
			},
		},
	})
}
