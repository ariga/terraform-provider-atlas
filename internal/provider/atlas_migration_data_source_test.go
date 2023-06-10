package provider_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

func TestAccMigrationDataSource(t *testing.T) {
	schema := "test"
	tempSchemas(t, mysqlURL, schema)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
				data "atlas_migration" "hello" {
					dir = "migrations?format=atlas"
					url = "%s/%s"
				}
				`, mysqlURL, schema),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.atlas_migration.hello", "id", "file://migrations?format=atlas"),
					resource.TestCheckResourceAttr("data.atlas_migration.hello", "status", "PENDING"),
					resource.TestCheckResourceAttr("data.atlas_migration.hello", "current", ""),
					resource.TestCheckResourceAttr("data.atlas_migration.hello", "next", "20221101163823"),
					resource.TestCheckResourceAttr("data.atlas_migration.hello", "latest", "20221101165415"),
				),
			},
		},
	})
	// Invalid hash
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
				data "atlas_migration" "hello" {
					dir = "migrations-hash?format=atlas"
					url = "%s/%s"
				}
				`, mysqlURL, schema),
				ExpectError: regexp.MustCompile("You have a checksum error in your migration directory"),
			},
		},
	})
}
