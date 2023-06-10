package provider_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestAccMigrationDataSource_RemoteDir(t *testing.T) {
	var (
		dir migrate.MemDir
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	url = "%s/test"
	remote_dir {
		name = "test"
	}
	cloud {
		token = "aci_bearer_token"
		url   = "%s"
	}
}`, mysqlURL, srv.URL)
	)
	t.Cleanup(srv.Close)

	t.Run("NoPendingFiles", func(t *testing.T) {
		tempSchemas(t, mysqlURL, "test")
		resource.Test(t, resource.TestCase{
			PreCheck:                 func() { testAccPreCheck(t) },
			ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: config,
					Check: resource.ComposeAggregateTestCheckFunc(
						resource.TestCheckResourceAttr("data.atlas_migration.hello", "id", "remote_dir://test"),
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

		tempSchemas(t, mysqlURL, "test")
		resource.Test(t, resource.TestCase{
			PreCheck:                 func() { testAccPreCheck(t) },
			ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: config,
					Check: resource.ComposeAggregateTestCheckFunc(
						resource.TestCheckResourceAttr("data.atlas_migration.hello", "id", "remote_dir://test"),
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
	_, err = fmt.Fprintf(w, `{"data":{"dir":{"content":%q}}}`, base64.StdEncoding.EncodeToString(arc))
	require.NoError(t, err)
}
