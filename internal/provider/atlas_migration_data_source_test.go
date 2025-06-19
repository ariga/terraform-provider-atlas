package provider_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"ariga.io/atlas/sql/migrate"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/stretchr/testify/require"
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
				ExpectError: regexp.MustCompile("checksum mismatch"),
			},
		},
	})

	// With custom atlas.hcl and variables
	t.Setenv("DB_URL", mysqlURL)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
data "atlas_migration" "hello" {
	# The dir attribute is required to be set, and
	# can't be supplied from the atlas.hcl
	dir      = "file://migrations?format=atlas"
	env_name = "tf"
	config   = <<-HCL
variable "schema_name" {
	type = string
}
locals {
	db_url = getenv("DB_URL")
}
env {
	name = atlas.env
	url = urlsetpath(local.db_url, var.schema_name)
	migration {
		dir = "this-dir-does-not-exist-and-always-gets-overrides"
	}
}
HCL
	variables = jsonencode({
		schema_name = "test"
	})
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.atlas_migration.hello", "status", "PENDING"),
					resource.TestCheckResourceAttr("data.atlas_migration.hello", "current", ""),
					resource.TestCheckResourceAttr("data.atlas_migration.hello", "next", "20221101163823"),
					resource.TestCheckResourceAttr("data.atlas_migration.hello", "latest", "20221101165415"),
				),
			},
		},
	})
}

func TestAccMigrationDataSource_AtlasURL(t *testing.T) {
	var (
		dir   = migrate.MemDir{}
		dbURL = fmt.Sprintf("sqlite://%s?_fk=true", filepath.Join(t.TempDir(), "sqlite.db"))
		srv   = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var m struct {
				Query     string `json:"query"`
				Variables struct {
					Input json.RawMessage `json:"input"`
				} `json:"variables"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&m))
			switch {
			case strings.Contains(m.Query, "query"):
				writeDir(t, &dir, w)
			default:
				t.Fatalf("unexpected query: %s", m.Query)
			}
		}))
		config = fmt.Sprintf(`
data "atlas_migration" "hello" {
	url      = "%s"
	dir      = "atlas://test"
	env_name = "tf"
	config   = <<-HCL
atlas {
  cloud {
    token = "aci_bearer_token"
    url   = "%s"
  }
}
HCL
}`, dbURL, srv.URL)
	)
	t.Cleanup(srv.Close)

	t.Run("Dir-From-Remote_Dir", func(t *testing.T) {
		resource.Test(t, resource.TestCase{
			PreCheck:                 func() { testAccPreCheck(t) },
			ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`
data "atlas_migration" "hello" {
	url      = "%s"
	env_name = "tf"
	config   = <<-HCL
atlas {
  cloud {
    token = "aci_bearer_token"
    url   = "%s"
  }
}
HCL
	remote_dir {
		name = "test"
	}
}`, dbURL, srv.URL),
					Check: resource.ComposeAggregateTestCheckFunc(
						resource.TestCheckResourceAttr("data.atlas_migration.hello", "dir", "atlas://test?format=atlas"),
						resource.TestCheckResourceAttr("data.atlas_migration.hello", "status", "OK"),
						resource.TestCheckResourceAttr("data.atlas_migration.hello", "current", "No migration applied yet"),
						resource.TestCheckResourceAttr("data.atlas_migration.hello", "next", ""),
						resource.TestCheckNoResourceAttr("data.atlas_migration.hello", "latest"),
					),
				},
			},
		})
	})

	t.Run("NoPendingFiles", func(t *testing.T) {
		resource.Test(t, resource.TestCase{
			PreCheck:                 func() { testAccPreCheck(t) },
			ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: config,
					Check: resource.ComposeAggregateTestCheckFunc(
						resource.TestCheckResourceAttr("data.atlas_migration.hello", "dir", "atlas://test?format=atlas"),
						resource.TestCheckResourceAttr("data.atlas_migration.hello", "remote_dir.name", "test"),
						resource.TestCheckNoResourceAttr("data.atlas_migration.hello", "remote_dir.tag"),
						resource.TestCheckResourceAttr("data.atlas_migration.hello", "status", "OK"),
						resource.TestCheckResourceAttr("data.atlas_migration.hello", "current", "No migration applied yet"),
						resource.TestCheckResourceAttr("data.atlas_migration.hello", "next", ""),
						resource.TestCheckNoResourceAttr("data.atlas_migration.hello", "latest"),
					),
				},
			},
		})
	})

	t.Run("WithFiles", func(t *testing.T) {
		require.NoError(t, dir.WriteFile("1.sql", []byte("create table foo (id int)")))
		require.NoError(t, dir.WriteFile("2.sql", []byte("create table bar (id int)")))
		resource.Test(t, resource.TestCase{
			PreCheck:                 func() { testAccPreCheck(t) },
			ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: config,
					Check: resource.ComposeAggregateTestCheckFunc(
						resource.TestCheckResourceAttr("data.atlas_migration.hello", "status", "PENDING"),
						resource.TestCheckResourceAttr("data.atlas_migration.hello", "current", ""),
						resource.TestCheckResourceAttr("data.atlas_migration.hello", "next", "1"),
						resource.TestCheckResourceAttr("data.atlas_migration.hello", "latest", "2"),
					),
				},
			},
		})
	})
}

func writeDir(t *testing.T, dir migrate.Dir, w io.Writer) {
	// Checksum before archiving.
	hf, err := dir.Checksum()
	require.NoError(t, err)
	ht, err := hf.MarshalText()
	require.NoError(t, err)
	require.NoError(t, dir.WriteFile(migrate.HashFileName, ht))
	// Archive and send.
	arc, err := migrate.ArchiveDir(dir)
	require.NoError(t, err)
	_, err = fmt.Fprintf(w, `{"data":{"dirState":{"content":%q}}}`, base64.StdEncoding.EncodeToString(arc))
	require.NoError(t, err)
}
