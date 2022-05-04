package atlas

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"

	"ariga.io/atlas/sql/sqlclient"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

const (
	MYSQL_URL     = "mysql://root:pass@localhost:3306/test"
	MYSQL_DEV_URL = "mysql://root:pass@localhost:3307/test"
)

var testAccActionConfigCreate = fmt.Sprintf(`
data "atlas_schema" "market" {
  dev_db_url = "%s"
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
  url = "%s"
}
`, MYSQL_DEV_URL, MYSQL_URL)

var testAccActionConfigUpdate = fmt.Sprintf(`
data "atlas_schema" "market" {
  dev_db_url = "%s"
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
  url = "%s"
}
`, MYSQL_DEV_URL, MYSQL_URL)

func TestAccAtlasDatabase(t *testing.T) {
	resource.Test(t, resource.TestCase{
		Providers: map[string]*schema.Provider{
			"atlas": Provider(),
		},
		Steps: []resource.TestStep{
			{
				Config: testAccActionConfigCreate,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("atlas_schema.testdb", "id", MYSQL_URL),
				),
			},
			{
				Config: testAccActionConfigUpdate,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("atlas_schema.testdb", "id", MYSQL_URL),
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

const testAccValidSqliteSchema = `
resource "atlas_schema" "testdb" {
  hcl = <<-EOT
	table "orders" {
		schema = schema.main
		column "id" {
			null = true
			type = int
		}
	}
	schema "main" {
	}
	EOT
  url = "sqlite://database.sqlite3?cache=shared"
}
`

const testAccInvalidSqliteSchema = `
resource "atlas_schema" "testdb" {
  hcl = <<-EOT
	table "orders {
		schema = schema.main
		column "id" {
			null = true
			type = int
		}
	}
	schema "main" {
	}
	EOT
  url = "sqlite://database.sqlite3?cache=shared"
}
`

func TestAccInvalidSchemaReturnsError(t *testing.T) {
	defer func() {
		os.Remove("database.sqlite3")
	}()
	resource.Test(t, resource.TestCase{
		Providers: map[string]*schema.Provider{
			"atlas": Provider(),
		},
		Steps: []resource.TestStep{
			{
				Config:             testAccValidSqliteSchema,
				ExpectNonEmptyPlan: true,
			},
			{
				Config:      testAccInvalidSqliteSchema,
				ExpectError: regexp.MustCompile("schemahcl: failed decoding"),
			},
		},
	})

	url := "sqlite://database.sqlite3?cache=shared"
	cli, err := sqlclient.Open(context.Background(), url)
	if err != nil {
		t.Error(err)
		return
	}
	realm, err := cli.InspectRealm(context.Background(), nil)
	if err != nil {
		t.Error(err)
		return
	}

	tbl, ok := realm.Schemas[0].Table("orders")
	if !ok {
		t.Error("expected database to have table \"orders\"")
		return
	}
	if _, ok := tbl.Column("id"); !ok {
		t.Error(fmt.Errorf("expected database to have table \"orders\" but got: %s", realm.Schemas[0].Tables[0].Name))
		return
	}
}

const createTableStmt = `create table type_table
(
    tBit                        bit(10)              default 4                                              null,
    tInt                        int(10)              default 4                                               not null,
    tTinyInt                    tinyint(10)          default 8                                                   null
) CHARSET = utf8mb4 COLLATE utf8mb4_0900_ai_ci;`

var testAccSanity = fmt.Sprintf(`
data "atlas_schema" "sanity" {
  dev_db_url = "%s"
  src = <<-EOT
	table "type_table" {
		schema  = schema.test
		charset = "utf8mb4"
		collate = "utf8mb4_0900_ai_ci"
		column "tInt" {
			null    = false
			type    = int
			default = 4
		}
	}
	schema "test" {
		charset = "utf8mb4"
		collate = "utf8mb4_0900_ai_ci"
	}
	EOT
}
resource "atlas_schema" "testdb" {
  hcl = data.atlas_schema.sanity.hcl
  url = "%s"
}
`, MYSQL_DEV_URL, MYSQL_URL)

const sanityState = `table "type_table" {
  schema = schema.test
  column "tInt" {
    null    = false
    type    = int
    default = 4
  }
}
schema "test" {
  charset = "utf8mb4"
  collate = "utf8mb4_0900_ai_ci"
}
`

func TestAccRemoveColumns(t *testing.T) {
	resource.Test(t, resource.TestCase{
		Providers: map[string]*schema.Provider{
			"atlas": Provider(),
		},
		Steps: []resource.TestStep{
			{
				PreConfig: func() {
					cli, err := sqlclient.Open(context.Background(), MYSQL_URL)
					if err != nil {
						t.Error(err)
					}
					cli.DB.Exec(createTableStmt)
				},
				Config: testAccSanity,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("atlas_schema.testdb", "id", MYSQL_URL),
					resource.TestCheckResourceAttr("atlas_schema.testdb", "hcl", sanityState),
				),
			},
		},
	})
}
