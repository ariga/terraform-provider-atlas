package provider_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

func TestAccMigrationResource(t *testing.T) {
	var (
		schema1 = "test_1"
		schema2 = "test_2"
		schema3 = "test_3"
	)
	tempSchemas(t, mysqlURL, schema1, schema2, schema3)
	tempSchemas(t, mysqlDevURL, schema1, schema2, schema3)

	// Jump to one-by-one using the data source
	config := fmt.Sprintf(`
	data "atlas_migration" "hello" {
		dir = "migrations?format=atlas"
		url = "%[1]s"
	}
	resource "atlas_migration" "testdb" {
		dir     = "migrations?format=atlas"
		version = data.atlas_migration.hello.next
		url     = "%[1]s"
	}`, fmt.Sprintf("%s/%s", mysqlURL, schema1))
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             config,
				ExpectNonEmptyPlan: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("atlas_migration.testdb", "status.current", "20221101163823"),
					resource.TestCheckResourceAttr("atlas_migration.testdb", "status.next", "20221101163841"),
				),
			},
			{
				Config:             config,
				ExpectNonEmptyPlan: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("atlas_migration.testdb", "status.current", "20221101163841"),
					resource.TestCheckResourceAttr("atlas_migration.testdb", "status.next", "20221101164227"),
				),
			},
		},
	})

	// Jump to the latest version
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
				data "atlas_migration" "hello" {
					dir = "migrations?format=atlas"
					url = "%[1]s"
				}
				resource "atlas_migration" "testdb" {
					dir     = "migrations?format=atlas"
					version = data.atlas_migration.hello.latest
					url     = "%[1]s"
				}`, fmt.Sprintf("%s/%s", mysqlURL, schema2)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("atlas_migration.testdb", "status.current", "20221101165415"),
					resource.TestCheckNoResourceAttr("atlas_migration.testdb", "status.next"),
				),
			},
		},
	})

	// Jump to the version 20221101164227
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
				resource "atlas_migration" "testdb" {
					dir     = "migrations?format=atlas"
					version = "20221101164227"
					url     = "%[1]s"
				}`, fmt.Sprintf("%s/%s", mysqlURL, schema3)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("atlas_migration.testdb", "status.current", "20221101164227"),
					resource.TestCheckResourceAttr("atlas_migration.testdb", "status.next", "20221101165036"),
				),
			},
		},
	})

	// Jump to the unknown version
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
				resource "atlas_migration" "testdb" {
					dir     = "migrations?format=atlas"
					version = "not-in-the-list"
					url     = "%[1]s"
				}`, fmt.Sprintf("%s/%s", mysqlURL, schema3)),
				ExpectError: regexp.MustCompile("The version is not found in the pending migrations"),
			},
		},
	})

	// Return error from atlas-cli
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
				resource "atlas_migration" "testdb" {
					dir     = "migrations-hash?format=atlas"
					version = "20221101163823"
					url     = "%[1]s"
				}`, fmt.Sprintf("%s/%s", mysqlURL, schema3)),
				ExpectError: regexp.MustCompile("checksum mismatch"),
			},
		},
	})
}

func TestAccMigrationResource_Dirty(t *testing.T) {
	schema := "test"
	schemaURL := fmt.Sprintf("%s/%s", mysqlURL, schema)
	config := fmt.Sprintf(`
	data "atlas_migration" "hello" {
		dir = "migrations?format=atlas"
		url = "%[1]s"
	}
	resource "atlas_migration" "testdb" {
		dir     = "migrations?format=atlas"
		version = data.atlas_migration.hello.latest
		url     = "%[1]s"
	}`, schemaURL)

	createTables(t, tempSchemas(t, mysqlURL, schema, "atlas_schema_revisions"),
		`create table atlas_schema_revisions.atlas_schema_revisions(version varchar(255) not null primary key);`)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile("We couldn't find a revision table in the connected schema but found one in"),
			},
		},
	})
}
