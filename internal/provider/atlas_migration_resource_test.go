package provider_test

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/stretchr/testify/require"
)

func TestAccMigrationResource(t *testing.T) {
	var (
		schema1 = "test_1"
		schema2 = "test_2"
		schema3 = "test_3"
		schema4 = "test_4"
	)
	tempSchemas(t, mysqlURL, schema1, schema2, schema3, schema4)
	tempSchemas(t, mysqlDevURL, schema1, schema2, schema3, schema4)

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

	// Handle unknown URL
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"foo": newFooProvider("foo", "mirror"),
		},
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
					resource "foo_mirror" "schema" {
						value = "%s"
					}
					data "atlas_migration" "hello" {
						dir = "migrations?format=atlas"
						url = format("%s/%%s", foo_mirror.schema.result)
					}
					resource "atlas_migration" "testdb" {
						dir     = "migrations?format=atlas"
						version = data.atlas_migration.hello.latest
						url     = data.atlas_migration.hello.url
					}`, schema3, mysqlURL),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("atlas_migration.testdb", "status.current", "20221101165415"),
					resource.TestCheckNoResourceAttr("atlas_migration.testdb", "status.next"),
				),
			},
		},
	})

	// Lint-Syntax
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
				resource "atlas_migration" "testdb" {
					dir     = "migrations-syntax?format=atlas"
					version = "20221101163823"
					url     = "%[1]s"
					dev_url = "%[2]s"
				}`,
					fmt.Sprintf("%s/%s", mysqlURL, schema4),
					fmt.Sprintf("%s/%s", mysqlDevURL, schema4),
				),
				ExpectError: regexp.MustCompile("an error in your SQL syntax"),
			},
		},
	})
}

func TestAccMigrationResource_WithLatestVersion(t *testing.T) {
	schema := "test_1"
	tempSchemas(t, mysqlURL, schema)

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
				}`, fmt.Sprintf("%s/%s", mysqlURL, schema)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("atlas_migration.testdb", "status.current", "20221101165415"),
					resource.TestCheckNoResourceAttr("atlas_migration.testdb", "status.next"),
				),
			},
		},
	})

	// Create new resource with the latest version already applied
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
				resource "atlas_migration" "testdb" {
					dir     = "migrations?format=atlas"
					version = "20221101165415"
					url     = "%[1]s"
				}`, fmt.Sprintf("%s/%s", mysqlURL, schema)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("atlas_migration.testdb", "status.current", "20221101165415"),
					resource.TestCheckNoResourceAttr("atlas_migration.testdb", "status.next"),
				),
			},
		},
	})
}

func TestAccMigrationResource_NoLongerExists(t *testing.T) {
	schema := "test_1"
	c := tempSchemas(t, mysqlURL, schema)
	config := fmt.Sprintf(`
	data "atlas_migration" "hello" {
		dir = "migrations?format=atlas"
		url = "%s/%s"
	}
	resource "atlas_migration" "testdb" {
		dir     = "migrations?format=atlas"
		version = data.atlas_migration.hello.latest
		url     = data.atlas_migration.hello.url
	}`, mysqlURL, schema)

	// Jump to the latest version
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             config,
				ExpectNonEmptyPlan: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("atlas_migration.testdb", "status.current", "20221101165415"),
					resource.TestCheckNoResourceAttr("atlas_migration.testdb", "status.next"),
					func(s *terraform.State) (err error) {
						// Drop the schema to simulate the schema no longer exists
						// in the database
						ctx := context.Background()
						_, err = c.ExecContext(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", schema))
						require.NoError(t, err)
						_, err = c.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", schema))
						require.NoError(t, err)
						return
					},
				),
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

func newFooProvider(name, resource string) func() (*schema.Provider, error) {
	return func() (*schema.Provider, error) {
		return &schema.Provider{
			Schema: map[string]*schema.Schema{},
			ResourcesMap: map[string]*schema.Resource{
				fmt.Sprintf("%s_%s", name, resource): {
					Schema: map[string]*schema.Schema{
						"value": {
							Type:     schema.TypeString,
							Required: true,
						},
						"result": {
							Type:     schema.TypeString,
							Computed: true,
						},
					},
					Read: func(d *schema.ResourceData, meta interface{}) error {
						d.Set("result", d.Get("value"))
						return nil
					},
					Create: func(d *schema.ResourceData, meta interface{}) error {
						d.SetId("none")
						d.Set("result", d.Get("value"))
						return nil
					},
					Update: func(d *schema.ResourceData, meta interface{}) error {
						d.Set("result", d.Get("value"))
						return nil
					},
					Delete: func(d *schema.ResourceData, meta interface{}) error {
						return nil
					},
				},
			},
		}, nil
	}
}
