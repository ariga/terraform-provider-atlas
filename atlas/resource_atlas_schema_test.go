package atlas

import (
	"context"
	"fmt"
	"testing"

	"ariga.io/atlas/sql/sqlclient"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

const testAccActionConfigCreate = `
data "atlas_schema" "market" {
  dev_db_url = "mysql://root:pass@localhost:3307"
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
  url = "mysql://root:pass@localhost:3306"
}
`

const testAccActionConfigUpdate = `
data "atlas_schema" "market" {
  dev_db_url = "mysql://root:pass@localhost:3307"
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
  url = "mysql://root:pass@localhost:3306"
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
					resource.TestCheckResourceAttr("atlas_schema.testdb", "id", "mysql://root:pass@localhost:3306"),
				),
			},
			{
				Config: testAccActionConfigUpdate,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("atlas_schema.testdb", "id", "mysql://root:pass@localhost:3306"),
					func(s *terraform.State) error {
						res := s.RootModule().Resources["atlas_schema.testdb"]
						cli, err := sqlclient.Open(context.TODO(), res.Primary.ID)
						if err != nil {
							return err
						}
						realm, err := cli.InspectRealm(context.TODO(), nil)
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

func TestAccDestroySchemas(t *testing.T) {
	// Create schemas "main" and "do-not-delete".
	preExistingSchema := `resource "atlas_schema" "testdb" {
		hcl = <<-EOT
		schema "do-not-delete" {}
		schema "main" {}
		EOT
		url = "mysql://root:pass@localhost:3306"
	}`
	// when the following destroys - it only deletes schema "main"
	tfSchema := `resource "atlas_schema" "testdb" {
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
		url = "mysql://root:pass@localhost:3306/main"
	}`
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
			url := "mysql://root:pass@localhost:3306"
			cli, err := sqlclient.Open(context.Background(), url)
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
			if _, ok := realm.Schema("main"); ok {
				return fmt.Errorf("schema 'main' wasn't deleted.")
			}
			return nil
		},
	})
}
