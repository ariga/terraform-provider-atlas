package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
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

func TestAccSchemaDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Read testing
			{
				Config: testAccData,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.atlas_schema.market", "hcl", normalHCL),
					resource.TestCheckResourceAttr("data.atlas_schema.market", "id", "/WWD4tjYzwMDMHxlNwuhrg"),
				),
			},
		},
	})
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Read testing
			{
				Config: `data "atlas_schema" "market" {
	dev_db_url = "mysql://root:pass@localhost:3307/test"
	src = ""
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.atlas_schema.market", "hcl", ""),
					resource.TestCheckResourceAttr("data.atlas_schema.market", "id", "bGInLge7AUJiuCF1YpXFjQ"),
				),
			},
		},
	})
}
