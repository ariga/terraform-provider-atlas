package atlas

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"ariga.io/atlas/sql/sqlclient"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

const (
	mysqlURL    = "mysql://root:pass@localhost:3306"
	mysqlDevURL = "mysql://root:pass@localhost:3307"
)

func TestAccAtlasDatabase(t *testing.T) {
	var testAccActionConfigCreate = fmt.Sprintf(`
data "atlas_schema" "market" {
  dev_db_url = "%s"
  src = <<-EOT
	schema "test1" {
		charset = "utf8mb4"
		collate = "utf8mb4_0900_ai_ci"
	}
	table "foo" {
		schema = schema.test1
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
  url = "%s"
}
`, mysqlDevURL, mysqlURL)

	var testAccActionConfigUpdate = fmt.Sprintf(`
data "atlas_schema" "market" {
  dev_db_url = "%s"
  src = <<-EOT
	schema "test1" {
		charset = "utf8mb4"
		collate = "utf8mb4_0900_ai_ci"
	}
	table "foo" {
		schema = schema.test1
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
  url = "%s"
}
`, mysqlDevURL, mysqlURL)
	resource.Test(t, resource.TestCase{
		Providers: map[string]*schema.Provider{
			"atlas": Provider(),
		},
		Steps: []resource.TestStep{
			{
				Config: testAccActionConfigCreate,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("atlas_schema.testdb", "id", mysqlURL),
				),
			},
			{
				Config: testAccActionConfigUpdate,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("atlas_schema.testdb", "id", mysqlURL),
					func(s *terraform.State) error {
						res := s.RootModule().Resources["atlas_schema.testdb"]
						cli, err := sqlclient.Open(context.Background(), res.Primary.ID)
						if err != nil {
							return err
						}
						realm, err := cli.InspectRealm(context.Background(), nil)
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

func TestAccInvalidSchemaReturnsError(t *testing.T) {
	testAccValidSchema := fmt.Sprintf(`
	resource "atlas_schema" "testdb" {
	  hcl = <<-EOT
		schema "test2" {
			charset = "utf8mb4"
			collate = "utf8mb4_0900_ai_ci"
		}
		table "foo" {
			schema = schema.test2
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
	  url = "%s"
	}
	`, mysqlURL)
	// invalid hcl file (missing `"` in 'table "orders...')
	testAccInvalidSchema := fmt.Sprintf(`
	resource "atlas_schema" "testdb" {
	  hcl = <<-EOT
		schema "test2" {
			charset = "utf8mb4"
			collate = "utf8mb4_0900_ai_ci"
		}
		table "orders {
			schema = schema.test2
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
	  url = "%s"
	}
	`, mysqlURL)

	resource.Test(t, resource.TestCase{
		Providers: map[string]*schema.Provider{
			"atlas": Provider(),
		},
		IsUnitTest: true,
		Steps: []resource.TestStep{
			{
				Config:             testAccValidSchema,
				ExpectNonEmptyPlan: true,
				Destroy:            false,
			},
			{
				Config:             testAccInvalidSchema,
				ExpectError:        regexp.MustCompile("schemahcl: failed decoding"),
				Destroy:            false,
				ExpectNonEmptyPlan: true,
				Check: func(s *terraform.State) error {
					cli, err := sqlclient.Open(context.Background(), mysqlURL)
					if err != nil {
						return err
					}
					realm, err := cli.InspectRealm(context.Background(), nil)
					if err != nil {
						return err
					}
					tbl, ok := realm.Schemas[0].Table("orders")
					if !ok {
						return fmt.Errorf("expected database to have table \"orders\"")
					}
					if _, ok := tbl.Column("id"); !ok {
						return fmt.Errorf("expected database to have table \"orders\" but got: %s", realm.Schemas[0].Tables[0].Name)
					}
					return nil
				},
			},
		},
	})
}

func TestAccRemoveColumns(t *testing.T) {
	const createTableStmt = `create table type_table
(
  tBit           bit(10)           default 4          null,
  tInt           int(10)           default 4      not null,
  tTinyInt       tinyint(10)       default 8          null
) CHARSET = utf8mb4 COLLATE utf8mb4_0900_ai_ci;`

	var testAccSanity = fmt.Sprintf(`
data "atlas_schema" "sanity" {
  dev_db_url = "%s"
  src = <<-EOT
	table "type_table" {
		schema  = schema.test3
		charset = "utf8mb4"
		collate = "utf8mb4_0900_ai_ci"
		column "tInt" {
			null    = false
			type    = int
			default = 4
		}
	}
	schema "test3" {
		charset = "utf8mb4"
		collate = "utf8mb4_0900_ai_ci"
	}
	EOT
}
resource "atlas_schema" "testdb" {
  hcl = data.atlas_schema.sanity.hcl
  url = "%s"
}
`, mysqlDevURL, mysqlURL)

	const sanityState = `table "type_table" {
  schema = schema.test3
  column "tInt" {
    null    = false
    type    = int
    default = 4
  }
}
schema "test3" {
  charset = "utf8mb4"
  collate = "utf8mb4_0900_ai_ci"
}
`
	resource.Test(t, resource.TestCase{
		Providers: map[string]*schema.Provider{
			"atlas": Provider(),
		},
		Steps: []resource.TestStep{
			{
				PreConfig: func() {
					cli, err := sqlclient.Open(context.Background(), mysqlURL)
					if err != nil {
						t.Error(err)
					}
					defer cli.Close()
					_, err = cli.DB.Exec("CREATE DATABASE IF NOT EXISTS test3;")
					if err != nil {
						t.Error(err)
					}
					_, err = cli.DB.Exec("USE test3;")
					if err != nil {
						t.Error(err)
					}
					_, err = cli.DB.Exec(createTableStmt)
					if err != nil {
						t.Error(err)
					}
				},
				Config: testAccSanity,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("atlas_schema.testdb", "id", mysqlURL),
					resource.TestCheckResourceAttr("atlas_schema.testdb", "hcl", sanityState),
				),
			},
		},
	})
}

func TestAccDestroySchemas(t *testing.T) {
	// Create schemas "test4" and "do-not-delete".
	preExistingSchema := fmt.Sprintf(`resource "atlas_schema" "testdb" {
		hcl = <<-EOT
		schema "do-not-delete" {}
		schema "test4" {}
		EOT
		url = "%s"
	}`, mysqlURL)
	// When the following destroys, it only deletes schema "test4".
	tfSchema := fmt.Sprintf(`resource "atlas_schema" "testdb" {
		hcl = <<-EOT
		table "orders" {
			schema = schema.test4
			column "id" {
				null = true
				type = int
			}
		}
		schema "test4" {
		}
		EOT
		url = "%s/test4"
	}`, mysqlURL)
	resource.Test(t, resource.TestCase{
		Providers: map[string]*schema.Provider{
			"atlas": Provider(),
		},
		Steps: []resource.TestStep{
			{
				Config:  preExistingSchema,
				Destroy: false,
				// ignore non-normalized schema
				ExpectNonEmptyPlan: true,
			},
			{
				Config: tfSchema,
				// ignore non-normalized schema
				ExpectNonEmptyPlan: true,
			},
		},
		CheckDestroy: func(s *terraform.State) error {
			cli, err := sqlclient.Open(context.Background(), mysqlURL)
			if err != nil {
				return err
			}
			realm, err := cli.InspectRealm(context.Background(), nil)
			if err != nil {
				return err
			}
			if _, ok := realm.Schema("do-not-delete"); !ok {
				return fmt.Errorf("schema 'do-not-delete' does not exist, but expected to not be destroyed.")
			}
			if _, ok := realm.Schema("test4"); ok {
				return fmt.Errorf("schema 'test4' wasn't deleted.")
			}
			return nil
		},
	})
}

func TestAccMultipleSchemas(t *testing.T) {
	mulSchema := fmt.Sprintf(`resource "atlas_schema" "testdb" {
		hcl = <<-EOT
		schema "m_test1" {}
		schema "m_test2" {}
		schema "m_test3" {}
		schema "m_test4" {}
		schema "m_test5" {}
		EOT
		url = "%s"
	}`, mysqlURL)
	resource.Test(t, resource.TestCase{
		Providers: map[string]*schema.Provider{
			"atlas": Provider(),
		},
		Steps: []resource.TestStep{
			{
				Config:  mulSchema,
				Destroy: false,
				// ignore non-normalized schema
				ExpectNonEmptyPlan: true,
				Check: func(s *terraform.State) error {
					cli, err := sqlclient.Open(context.Background(), mysqlURL)
					if err != nil {
						return err
					}
					realm, err := cli.InspectRealm(context.Background(), nil)
					if err != nil {
						return err
					}
					schemas := []string{"m_test1", "m_test2", "m_test3", "m_test4", "m_test5"}
					for _, s := range schemas {
						if _, ok := realm.Schema(s); !ok {
							return fmt.Errorf("schema '%s' does not exist.", s)
						}
					}
					return nil
				},
			},
		},
		CheckDestroy: func(s *terraform.State) error {
			cli, err := sqlclient.Open(context.Background(), mysqlURL)
			if err != nil {
				return err
			}
			realm, err := cli.InspectRealm(context.Background(), nil)
			if err != nil {
				return err
			}
			schemas := []string{"m_test1", "m_test2", "m_test3", "m_test4", "m_test5"}
			for _, s := range schemas {
				if _, ok := realm.Schema(s); ok {
					return fmt.Errorf("schema '%s' exists.", s)
				}
			}
			return nil
		},
	})
}
