package provider_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	atlas "ariga.io/atlas-go-sdk/atlasexec"
	"ariga.io/atlas/sql/migrate"
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

	// Jump to the latest version, then down to the version 20221101164227
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
				ExpectError: regexp.MustCompile(`version "not-in-the-list" not found`),
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
					resource "foo_mirror" "dir" {
						value = "migrations"
					}
					resource "foo_mirror" "schema" {
						value = "%s"
					}
					data "atlas_migration" "hello" {
						dir = "${foo_mirror.dir.result}?format=atlas"
						url = format("%s/%%s", foo_mirror.schema.result)
					}
					resource "atlas_migration" "testdb" {
						dir     = "${foo_mirror.dir.result}?format=atlas"
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
				ExpectError: regexp.MustCompile("error in your SQL syntax"),
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

func TestAccMigrationResource_AtlasURL(t *testing.T) {
	var (
		dir   = migrate.MemDir{}
		dbURL = fmt.Sprintf("sqlite://%s?_fk=true", filepath.Join(t.TempDir(), "sqlite.db"))
		srv   = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			type (
				Input struct {
					Context atlas.DeployRunContext `json:"context,omitempty"`
				}
				GraphQLQuery struct {
					Query     string `json:"query"`
					Variables struct {
						Input json.RawMessage `json:"input"`
					} `json:"variables"`
				}
			)
			var m GraphQLQuery
			require.NoError(t, json.NewDecoder(r.Body).Decode(&m))
			switch {
			case strings.Contains(m.Query, "query"):
				writeDir(t, &dir, w)
			case strings.Contains(m.Query, "reportMigration"):
				var i Input
				err := json.Unmarshal(m.Variables.Input, &i)
				require.NoError(t, err)
				require.Equal(t, "0.0.0-test", i.Context.TriggerVersion)
				require.Equal(t, atlas.TriggerTypeTerraform, i.Context.TriggerType)
				fmt.Fprint(w, `{"data":{"reportMigration":{"success":true}}}`)
			default:
				t.Fatalf("unexpected query: %s", m.Query)
			}
		}))
		config = fmt.Sprintf(`
		provider "atlas" {
			cloud {
				token   = "aci_bearer_token"
				url     = "%[1]s"
				project = "test"
			}
		}
		data "atlas_migration" "hello" {
			url = "%[2]s"
			dir = "atlas://test"
		}
		resource "atlas_migration" "testdb" {
			url     = "%[2]s"
			version = data.atlas_migration.hello.next
			dir     = data.atlas_migration.hello.dir
		}
		`, srv.URL, dbURL)
	)
	t.Cleanup(srv.Close)
	t.Run("NoPendingFiles", func(t *testing.T) {
		resource.Test(t, resource.TestCase{
			PreCheck:                 func() { testAccPreCheck(t) },
			ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: config,
					Check: resource.ComposeAggregateTestCheckFunc(
						resource.TestCheckResourceAttr("atlas_migration.testdb", "status.current", "No migration applied yet"),
						resource.TestCheckNoResourceAttr("atlas_migration.testdb", "status.next"),
					),
				},
			},
		})
	})
	t.Run("WithPendingFiles", func(t *testing.T) {
		require.NoError(t, dir.WriteFile("1.sql", []byte("create table foo (id int)")))
		require.NoError(t, dir.WriteFile("2.sql", []byte("create table bar (id int)")))
		resource.Test(t, resource.TestCase{
			PreCheck:                 func() { testAccPreCheck(t) },
			ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config:             config,
					ExpectNonEmptyPlan: true,
					Check: resource.ComposeAggregateTestCheckFunc(
						resource.TestCheckResourceAttr("atlas_migration.testdb", "id", "remote_dir://test"),
						resource.TestCheckResourceAttr("atlas_migration.testdb", "status.current", "1"),
						resource.TestCheckResourceAttr("atlas_migration.testdb", "status.latest", "2"),
						resource.TestCheckResourceAttr("atlas_migration.testdb", "status.next", "2"),
					),
				},
				{
					Config:             config,
					ExpectNonEmptyPlan: true,
					Check: resource.ComposeAggregateTestCheckFunc(
						resource.TestCheckResourceAttr("atlas_migration.testdb", "id", "remote_dir://test"),
						resource.TestCheckResourceAttr("atlas_migration.testdb", "status.current", "2"),
						resource.TestCheckResourceAttr("atlas_migration.testdb", "status.latest", "2"),
						resource.TestCheckNoResourceAttr("atlas_migration.testdb", "status.next"),
					),
				},
			},
		})
	})
}

func TestAccMigrationResource_AtlasURL_WithTag(t *testing.T) {
	var (
		byTag  = make(map[string]migrate.Dir)
		dbURL  = fmt.Sprintf("sqlite://%s?_fk=true", filepath.Join(t.TempDir(), "sqlite.db"))
		devURL = "sqlite://file::memory:?cache=shared"
		srv    = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			type (
				Input struct {
					Context atlas.DeployRunContext `json:"context,omitempty"`
				}
				GraphQLQuery struct {
					Query     string `json:"query"`
					Variables struct {
						Input json.RawMessage `json:"input"`
					} `json:"variables"`
				}
			)
			var m GraphQLQuery
			require.NoError(t, json.NewDecoder(r.Body).Decode(&m))
			switch {
			case strings.Contains(m.Query, "query"):
				switch {
				case strings.Contains(m.Query, "Bot"):
				case strings.Contains(m.Query, "dirState"):
					var input struct {
						Name string `json:"name"`
						Tag  string `json:"tag"`
					}
					require.NoError(t, json.Unmarshal(m.Variables.Input, &input))
					dir, ok := byTag[input.Tag]
					if !ok {
						fmt.Fprintf(w, `{"errors":[{"message":"not found"}]}`)
						return
					}
					writeDir(t, dir, w)
				}
			case strings.Contains(m.Query, "reportMigration"):
				var i Input
				err := json.Unmarshal(m.Variables.Input, &i)
				require.NoError(t, err)
				require.Equal(t, "0.0.0-test", i.Context.TriggerVersion)
				require.Equal(t, atlas.TriggerTypeTerraform, i.Context.TriggerType)
				fmt.Fprint(w, `{"data":{"reportMigration":{"success":true}}}`)
			default:
				t.Fatalf("unexpected query: %s", m.Query)
			}
		}))
	)
	t.Cleanup(srv.Close)
	latest, err := migrate.NewLocalDir(t.TempDir())
	require.NoError(t, err)
	byTag["latest"] = latest
	byTag[""] = latest
	require.NoError(t, latest.WriteFile("1.sql", []byte("create table t1 (id int)")))
	require.NoError(t, latest.WriteFile("2.sql", []byte("create table t2 (id int)")))
	tag1, err := migrate.NewLocalDir(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, tag1.WriteFile("1.sql", []byte("create table t1 (id int)")))
	byTag["one-down"] = tag1
	// migrate to the latest version
	config := fmt.Sprintf(`
	provider "atlas" {
		dev_url = "%[1]s"
		cloud {
			token   = "aci_bearer_token"
			url     = "%[2]s"
			project = "test"
		}
	}
	resource "atlas_migration" "hello" {
		url = "%[3]s"
		dir = "atlas://test"
	}
	`, devURL, srv.URL, dbURL)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("atlas_migration.hello", "id", "remote_dir://test"),
					resource.TestCheckResourceAttr("atlas_migration.hello", "dir", "atlas://test"),
					resource.TestCheckResourceAttr("atlas_migration.hello", "status.current", "2"),
					resource.TestCheckResourceAttr("atlas_migration.hello", "status.latest", "2"),
					resource.TestCheckNoResourceAttr("atlas_migration.hello", "status.next"),
				),
			},
		},
	})
	// down to the specific tag
	config = fmt.Sprintf(`
	provider "atlas" {
		dev_url = "%[1]s"
		cloud {
			token   = "aci_bearer_token"
			url     = "%[2]s"
			project = "test"
		}
	}
	resource "atlas_migration" "hello" {
		url = "%[3]s"
		dir = "atlas://test?tag=one-down"
	}
	`, devURL, srv.URL, dbURL)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("atlas_migration.hello", "id", "remote_dir://test"),
					resource.TestCheckResourceAttr("atlas_migration.hello", "dir", "atlas://test?tag=one-down"),
					resource.TestCheckResourceAttr("atlas_migration.hello", "status.current", "1"),
					resource.TestCheckResourceAttr("atlas_migration.hello", "status.latest", "1"),
					resource.TestCheckNoResourceAttr("atlas_migration.hello", "status.next"),
				),
			},
		},
	})
	// back to the latest version
	config = fmt.Sprintf(`
	provider "atlas" {
		dev_url = "%[1]s"
		cloud {
			token   = "aci_bearer_token"
			url     = "%[2]s"
			project = "test"
		}
	}
	resource "atlas_migration" "hello" {
		url = "%[3]s"
		dir = "atlas://test?tag=latest"
	}`, devURL, srv.URL, dbURL)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("atlas_migration.hello", "id", "remote_dir://test"),
					resource.TestCheckResourceAttr("atlas_migration.hello", "dir", "atlas://test?tag=latest"),
					resource.TestCheckResourceAttr("atlas_migration.hello", "status.current", "2"),
					resource.TestCheckResourceAttr("atlas_migration.hello", "status.latest", "2"),
					resource.TestCheckNoResourceAttr("atlas_migration.hello", "status.next"),
				),
			},
		},
	})
}

func TestAccMigrationResource_RequireApproval(t *testing.T) {
	type (
		DeploymentApprovalsStatus string
		MigFile                   struct {
			Name    string
			Content []byte
		}
	)
	const (
		PlanPendingApproval DeploymentApprovalsStatus = "PENDING_USER"
		PlanApproved        DeploymentApprovalsStatus = "APPROVED"
		PlanAborted         DeploymentApprovalsStatus = "ABORTED"
		PlanApplied         DeploymentApprovalsStatus = "APPLIED"
	)
	var (
		flow   []*DeploymentApprovalsStatus
		byTag  = make(map[string]migrate.Dir)
		devURL = "sqlite://file::memory:?cache=shared"
		dbURL  = fmt.Sprintf("sqlite://%s?_fk=true", filepath.Join(t.TempDir(), "sqlite.db"))
		srv    = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			type (
				GraphQLQuery struct {
					Query     string `json:"query"`
					Variables struct {
						Input json.RawMessage `json:"input"`
					} `json:"variables"`
				}
			)
			var m GraphQLQuery
			require.NoError(t, json.NewDecoder(r.Body).Decode(&m))
			switch {
			case strings.Contains(m.Query, "query"):
				switch {
				case strings.Contains(m.Query, "Bot"):
				case strings.Contains(m.Query, "dirState"):
					var input struct {
						Name string `json:"name"`
						Tag  string `json:"tag"`
					}
					require.NoError(t, json.Unmarshal(m.Variables.Input, &input))
					dir, ok := byTag[input.Tag]
					if !ok {
						fmt.Fprintf(w, `{"errors":[{"message":"not found"}]}`)
						return
					}
					writeDir(t, dir, w)
				case strings.Contains(m.Query, "protectedFlows"):
					fmt.Fprintf(w, `{"data":{"dir":{"protectedFlows":{"migrateDown": true}}}}`)
				case strings.Contains(m.Query, "migratePlanByExtID"):
					require.NotEmpty(t, flow)
					status := flow[0]
					flow = flow[1:]
					if status != nil {
						fmt.Fprintf(w, `{"data":{"migratePlanByExtID":{"url": "https://gh.atlasgo.cloud/deployments/51539607559","status":%q}}}`, *status)
					} else {
						fmt.Fprintf(w, `{"data":{"migratePlanByExtID":null}}`)
					}
				case strings.Contains(m.Query, "migrateTargetByExtID"):
					fmt.Fprint(w, `{"data":{"migrateTargetByExtID":null}}`)
				default:
					t.Fatalf("unexpected query: %s", m.Query)
				}
			case strings.Contains(m.Query, "mutation"):
				switch {
				case strings.Contains(m.Query, "CreateMigrateDownPlan"):
					fmt.Fprint(w, `{"data":{"createMigrateDownPlan":{"url": "https://gh.atlasgo.cloud/deployments/51539607559"}}}`)
				case strings.Contains(m.Query, "ReportMigrationDown"):
					fmt.Fprint(w, `{"data":{"reportMigrationDown":{"url": "https://gh.atlasgo.cloud/deployments/51539607559"}}}`)
				case strings.Contains(m.Query, "ReportMigration"):
					fmt.Fprint(w, `{"data":{"reportMigration":{"success":true}}}`)
				default:
					t.Fatalf("unexpected mutation: %s", m.Query)
				}
			default:
				t.Fatalf("unexpected query: %s", m.Query)
			}
		}))
	)
	t.Cleanup(srv.Close)
	files := []MigFile{
		{"1.sql", []byte("create table t1 (id int)")},
		{"2.sql", []byte("create table t2 (id int)")},
		{"3.sql", []byte("create table t3 (id int)")},
		{"4.sql", []byte("create table t4 (id int)")},
	}
	latest, err := migrate.NewLocalDir(t.TempDir())
	require.NoError(t, err)
	for _, v := range files {
		require.NoError(t, latest.WriteFile(v.Name, v.Content))
	}
	tag3, err := migrate.NewLocalDir(t.TempDir())
	require.NoError(t, err)
	for _, v := range files[:3] {
		require.NoError(t, tag3.WriteFile(v.Name, v.Content))
	}
	tag2, err := migrate.NewLocalDir(t.TempDir())
	require.NoError(t, err)
	for _, v := range files[:2] {
		require.NoError(t, tag2.WriteFile(v.Name, v.Content))
	}
	tag1, err := migrate.NewLocalDir(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, tag1.WriteFile("1.sql", []byte("create table t1 (id int)")))
	byTag["latest"], byTag[""] = latest, latest
	byTag["tag3"] = tag3
	byTag["tag2"] = tag2
	byTag["tag1"] = tag1
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
				provider "atlas" {
					dev_url = "%[1]s"
					cloud {
						token   = "aci_bearer_token"
						url     = "%[2]s"
						project = "test"
					}
				}
				resource "atlas_migration" "hello" {
					url = "%[3]s"
					dir = "atlas://test?tag=latest"
				}`, devURL, srv.URL, dbURL),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("atlas_migration.hello", "id", "remote_dir://test"),
					resource.TestCheckResourceAttr("atlas_migration.hello", "status.current", "4"),
					resource.TestCheckResourceAttr("atlas_migration.hello", "status.latest", "4"),
					resource.TestCheckNoResourceAttr("atlas_migration.hello", "status.next"),
				),
			},
		},
	})
	newS := func(s DeploymentApprovalsStatus) *DeploymentApprovalsStatus { return &s }
	// plan is waiting for approval, and then approved
	flow = append(flow, newS(PlanPendingApproval), newS(PlanApproved))
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
				provider "atlas" {
					dev_url = "%[1]s"
					cloud {
						token   = "aci_bearer_token"
						url     = "%[2]s"
						project = "test"
					}
				}
				resource "atlas_migration" "hello" {
					url = "%[3]s"
					dir = "atlas://test?tag=tag3"
				}`, devURL, srv.URL, dbURL),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("atlas_migration.hello", "id", "remote_dir://test"),
					resource.TestCheckResourceAttr("atlas_migration.hello", "status.current", "3"),
					resource.TestCheckResourceAttr("atlas_migration.hello", "status.latest", "3"),
					resource.TestCheckNoResourceAttr("atlas_migration.hello", "status.next"),
				),
			},
		},
	})
	// plan is waiting for approval, and then aborted
	flow = append(flow, newS(PlanPendingApproval), newS(PlanAborted))
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
				provider "atlas" {
					dev_url = "%[1]s"
					cloud {
						token   = "aci_bearer_token"
						url     = "%[2]s"
						project = "test"
					}
				}
				resource "atlas_migration" "hello" {
					url = "%[3]s"
					dir = "atlas://test?tag=tag2"
				}`, devURL, srv.URL, dbURL),
				ExpectError: regexp.MustCompile("migration plan was aborted"),
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
