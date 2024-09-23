package provider_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"ariga.io/atlas/sql/sqlclient"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/stretchr/testify/require"

	"ariga.io/ariga/terraform-provider-atlas/internal/provider"
	atlas "ariga.io/atlas-go-sdk/atlasexec"
)

const (
	mysqlURL             = "mysql://root:pass@localhost:3306"
	mysqlDevURL          = "mysql://root:pass@localhost:3307"
	mysqlURLWithoutCreds = "mysql://localhost:3306"
)

func TestAccAtlasDatabase(t *testing.T) {
	tempSchemas(t, mysqlURL, "test")
	var testAccActionConfigCreate = fmt.Sprintf(`
resource "foo_mirror" "url" {
	value = "%s"
}
data "atlas_schema" "market" {
  dev_url = "%s"
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
  url = foo_mirror.url.result
  dev_url = "%s"
}
`, mysqlURL, mysqlDevURL, mysqlDevURL)

	var testAccActionConfigUpdate = fmt.Sprintf(`
data "atlas_schema" "market" {
  dev_url = "%s"
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
`, mysqlDevURL, mysqlURL)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"foo": newFooProvider("foo", "mirror"),
		},
		Steps: []resource.TestStep{
			{
				Config: testAccActionConfigCreate,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("atlas_schema.testdb", "id", mysqlURLWithoutCreds),
				),
			},
			{
				Config: testAccActionConfigUpdate,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("atlas_schema.testdb", "id", mysqlURLWithoutCreds),
					func(s *terraform.State) error {
						res := s.RootModule().Resources["atlas_schema.testdb"]
						cli, err := sqlclient.Open(context.Background(), res.Primary.Attributes["url"])
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
	tempSchemas(t, mysqlURL, "test")
	testAccValidSchema := fmt.Sprintf(`
	resource "atlas_schema" "testdb" {
	  hcl = <<-EOT
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
	  url = "%s"
	}
	`, mysqlURL)
	// invalid hcl file (missing `"` in 'table "orders...')
	testAccInvalidSchema := fmt.Sprintf(`
	resource "atlas_schema" "testdb" {
	  hcl = <<-EOT
		schema "test" {
			charset = "utf8mb4"
			collate = "utf8mb4_0900_ai_ci"
		}
		table "orders {
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
	  url = "%s"
	}
	`, mysqlURL)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		IsUnitTest:               true,
		Steps: []resource.TestStep{
			{
				Config:             testAccValidSchema,
				ExpectNonEmptyPlan: true,
				Destroy:            false,
			},
			{
				Config:             testAccInvalidSchema,
				ExpectError:        regexp.MustCompile("Invalid multi-line"),
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

func TestEmptyHCL(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		IsUnitTest:               true,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
				resource "atlas_schema" "testdb" {
					hcl = ""
					url = "%s"
				}
				`, mysqlURL),
				ExpectError: regexp.MustCompile("Error: Invalid Attribute Value Length"),
				Destroy:     false,
			},
		},
	})
}

func TestEnsureSyncOnFirstRun(t *testing.T) {
	tempSchemas(t, mysqlURL, "test1", "test2")
	hcl := fmt.Sprintf(`
	resource "atlas_schema" "new_schema" {
	  hcl = <<-EOT
schema "test1" {
  charset = "utf8mb4"
  collate = "utf8mb4_0900_ai_ci"
}
		EOT
	  url = "%s"
	}
	`, mysqlURL)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		IsUnitTest:               true,
		Steps: []resource.TestStep{
			{
				Config:      hcl,
				ExpectError: regexp.MustCompile("Error: Unrecognized schema resources"),
			},
		},
	})
}

func TestExcludeSchema(t *testing.T) {
	tempSchemas(t, mysqlURL, "test1", "test2", "test3")
	hcl := fmt.Sprintf(`
resource "atlas_schema" "new_schema" {
	hcl = <<-EOT
schema "test1" {
  charset = "utf8mb4"
  collate = "utf8mb4_0900_ai_ci"
}
EOT
	exclude = ["test2", "test3", ""]
	url = "%s"
}`, mysqlURL)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: hcl,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("atlas_schema.new_schema", "id", mysqlURLWithoutCreds),
				),
			},
		},
	})
	hcl = fmt.Sprintf(`
	resource "atlas_schema" "new_schema" {
		hcl = <<-EOT
schema "test1" {
  charset = "utf8mb4"
  collate = "utf8mb4_0900_ai_ci"
}
EOT
		exclude = [null]
		url = "%s"
	}`, mysqlURL)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      hcl,
				ExpectError: regexp.MustCompile("Value Conversion Error"),
			},
		},
	})
}

func TestAccRemoveColumns(t *testing.T) {
	tempSchemas(t, mysqlURL, "test")
	const createTableStmt = `create table test.type_table
(
  tBit           bit(10)                 not null,
  tInt           int(10)                 not null,
  tTinyInt       tinyint(10)             not null
) CHARSET = utf8mb4 COLLATE utf8mb4_0900_ai_ci;`

	var (
		steps = []string{
			`table "type_table" {
  schema = schema.test
  column "tBit" {
    null = false
    type = bit(10)
  }
  column "tInt" {
    null = false
    type = int
  }
  column "tTinyInt" {
    null = false
    type = tinyint
  }
}
schema "test" {
  charset = "utf8mb4"
  collate = "utf8mb4_0900_ai_ci"
}
`,
			`table "type_table" {
  schema = schema.test
  column "tInt" {
    null = false
    type = int
  }
}
schema "test" {
  charset = "utf8mb4"
  collate = "utf8mb4_0900_ai_ci"
}
`,
		}
		testAccSanityT = `
data "atlas_schema" "sanity" {
  dev_url = "%s"
  src = <<-EOT
	%s
	EOT
}
resource "atlas_schema" "testdb" {
  hcl = data.atlas_schema.sanity.hcl
  url = "%s"
}
`
	)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				PreConfig: func() {
					cli, err := sqlclient.Open(context.Background(), mysqlURL)
					if err != nil {
						t.Error(err)
					}
					defer cli.Close()
					_, err = cli.DB.Exec(createTableStmt)
					if err != nil {
						t.Error(err)
					}
				},
				Config: fmt.Sprintf(testAccSanityT, mysqlDevURL, steps[0], mysqlURL),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("atlas_schema.testdb", "id", mysqlURLWithoutCreds),
					resource.TestCheckResourceAttr("atlas_schema.testdb", "hcl", steps[0]),
				),
			},
			{
				Config: fmt.Sprintf(testAccSanityT, mysqlDevURL, steps[1], mysqlURL),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("atlas_schema.testdb", "id", mysqlURLWithoutCreds),
					resource.TestCheckResourceAttr("atlas_schema.testdb", "hcl", steps[1]),
				),
			},
		},
	})
}

func TestAccDestroySchemas(t *testing.T) {
	tempSchemas(t, mysqlURL, "test4", "do-not-delete")
	// Create schemas "test4" and "do-not-delete".
	preExistingSchema := fmt.Sprintf(`resource "atlas_schema" "testdb" {
		hcl = <<-EOT
		schema "do-not-delete" {}
		schema "test4" {}
		EOT
		url = "%s"
	}`, mysqlURL)
	schema := `resource "atlas_schema" "testdb" {
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
		url = "%s"
	}`
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:  preExistingSchema,
				Destroy: false,
				// ignore non-normalized schema
				ExpectNonEmptyPlan: true,
			},
			{
				// When the following destroys, it doesn't delete any schemas.
				// It only deletes the tables in the schemas.
				Config: fmt.Sprintf(schema, mysqlURL+"/test4"),
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
			if _, ok := realm.Schema("test4"); !ok {
				return fmt.Errorf("schema 'test4' does not exist.")
			}
			return nil
		},
	})
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:  preExistingSchema,
				Destroy: false,
				// ignore non-normalized schema
				ExpectNonEmptyPlan: true,
			},
			{
				// When the following destroys, it deletes all schemas.
				Config: fmt.Sprintf(schema, mysqlURL),
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
			if _, ok := realm.Schema("do-not-delete"); ok {
				return fmt.Errorf("schema 'do-not-delete' exist, but expected to be destroyed.")
			}
			if _, ok := realm.Schema("test4"); ok {
				return fmt.Errorf("schema 'test4' exist, but expected to be destroyed.")
			}
			return nil
		},
	})
}

func TestAccMultipleSchemas(t *testing.T) {
	tempSchemas(t, mysqlURL, "m_test1", "m_test2", "m_test3", "m_test4", "m_test5")
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
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
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

func tempSchemas(t *testing.T, url string, schemas ...string) *sqlclient.Client {
	t.Helper()
	c, err := sqlclient.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range schemas {
		_, err := c.ExecContext(context.Background(), fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", s))
		if err != nil {
			t.Errorf("failed creating schema: %s", err)
		}
	}
	drop(t, c, schemas...)
	return c
}

func createTables(t *testing.T, c *sqlclient.Client, tables ...string) {
	for _, tableDDL := range tables {
		_, err := c.ExecContext(context.Background(), tableDDL)
		if err != nil {
			t.Errorf("failed creating schema: %s", err)
		}
	}
}

func drop(t *testing.T, c *sqlclient.Client, schemas ...string) {
	t.Helper()
	t.Cleanup(func() {
		t.Helper()
		t.Log("Dropping all schemas")
		for _, s := range schemas {
			_, err := c.ExecContext(context.Background(), fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", s))
			if err != nil {
				t.Errorf("failed dropping schema: %s", err)
			}
		}
		if err := c.Close(); err != nil {
			t.Errorf("failed closing client: %s", err)
		}
	})
}

func TestPrintPlanSQL(t *testing.T) {
	type args struct {
		ctx  context.Context
		data *provider.AtlasSchemaResourceModel
	}
	tests := []struct {
		name      string
		args      args
		wantDiags diag.Diagnostics
	}{
		{
			args: args{
				ctx: context.Background(),
				data: &provider.AtlasSchemaResourceModel{
					URL:     types.StringValue(mysqlURL),
					DevURL:  types.StringValue(mysqlDevURL),
					Exclude: types.ListNull(types.StringType),
					HCL: types.StringValue(`schema "test" {
  charset = "utf8mb4"
  collate = "utf8mb4_0900_ai_ci"
}
table "orders" {
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
`),
				},
			},
			wantDiags: []diag.Diagnostic{
				diag.NewWarningDiagnostic("Atlas Plan",
					strings.Join([]string{
						"The following SQL statements will be executed:",
						"",
						"",
						"CREATE DATABASE `test`;",
						"CREATE TABLE `test`.`orders` (",
						"  `id` int NOT NULL AUTO_INCREMENT,",
						"  PRIMARY KEY (`id`)",
						") CHARSET utf8mb4 COLLATE utf8mb4_0900_ai_ci;",
						"",
					}, "\n")),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotDiags := provider.PrintPlanSQL(tt.args.ctx, &provider.ProviderData{
				Client: func(wd string, _ *provider.CloudConfig) (provider.AtlasExec, error) {
					return atlas.NewClient(wd, "atlas")
				},
				DevURL: mysqlDevURL,
			}, tt.args.data)
			require.Equal(t, tt.wantDiags, gotDiags)
		})
	}
}
