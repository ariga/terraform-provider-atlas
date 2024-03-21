package provider_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

const (
	testAccData = `variable "tenant" {
		type = string
	}
	schema "test" {
		name    = var.tenant
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
	}`
	normalHCL = `table "foo" {
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
)

func TestAccSchemaDataSource(t *testing.T) {
	tempSchemas(t, mysqlDevURL, "test")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Read testing
			{
				Config: fmt.Sprintf(`data "atlas_schema" "market" {
					dev_url = "mysql://root:pass@localhost:3307/test"
					variables = {
						tenant = "test",
					}
					src        = <<-EOT
					%s
					EOT
				}`, testAccData),
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
					dev_url = "mysql://root:pass@localhost:3307/test"
					src = ""
				}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("data.atlas_schema.market", "hcl"),
					resource.TestCheckResourceAttr("data.atlas_schema.market", "id", "bGInLge7AUJiuCF1YpXFjQ"),
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
					dev_url = "mysql://root:pass@localhost:3307/test"
					src     = "file://./sql-files/schema.sql"
				}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.atlas_schema.market", "hcl", normalHCL),
					resource.TestCheckResourceAttr("data.atlas_schema.market", "id", "/WWD4tjYzwMDMHxlNwuhrg"),
				),
			},
		},
	})
	// Use DevDB from provider config
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Read testing
			{
				Config: `
				provider "atlas" {
					dev_url = "mysql://root:pass@localhost:3307/test"
				}
				data "atlas_schema" "hello" {
					src = "file://./sql-files/schema.sql"
					dev_url = "mysql://root:pass@localhost:3307/test"
				}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.atlas_schema.hello", "hcl", normalHCL),
					resource.TestCheckResourceAttr("data.atlas_schema.hello", "id", "/WWD4tjYzwMDMHxlNwuhrg"),
				),
			},
		},
	})
}
