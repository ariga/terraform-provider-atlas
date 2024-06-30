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

	"ariga.io/atlas-go-sdk/atlasexec"
	"ariga.io/atlas/sql/migrate"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	sdkprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	sdkschema "github.com/hashicorp/terraform-plugin-framework/provider/schema"
	rs "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/stretchr/testify/require"

	"ariga.io/ariga/terraform-provider-atlas/internal/provider"
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

func TestAccMigrationResource_RemoteDir(t *testing.T) {
	var (
		dir   = migrate.MemDir{}
		dbURL = fmt.Sprintf("sqlite://%s?_fk=true", filepath.Join(t.TempDir(), "sqlite.db"))
		srv   = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			type (
				Input struct {
					Context atlasexec.DeployRunContext `json:"context,omitempty"`
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
				require.Equal(t, "test", i.Context.TriggerVersion)
				require.Equal(t, atlasexec.TriggerTypeTerraform, i.Context.TriggerType)
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
			remote_dir {
				name = "test"
			}
		}
		resource "atlas_migration" "testdb" {
			url = "%[2]s"
			version = data.atlas_migration.hello.next
			remote_dir {
				name = data.atlas_migration.hello.remote_dir.name
				tag  = data.atlas_migration.hello.remote_dir.tag
			}
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

func TestModifyPlan(t *testing.T) {
	type args struct {
		req  rs.ModifyPlanRequest
		resp rs.ModifyPlanResponse
	}

	tests := []struct {
		name string
		args args
	}{
		{
			name: "no-change",
			args: args{
				req: rs.ModifyPlanRequest{
					Plan: tfsdk.Plan{
						Raw: tftypes.NewValue(tftypes.Object{
							AttributeTypes: map[string]tftypes.Type{
								"url":              tftypes.String,
								"dev_url":          tftypes.String,
								"dir":              tftypes.String,
								"revisions_schema": tftypes.String,
								"version":          tftypes.String,
								"baseline":         tftypes.String,
								"exec_order":       tftypes.String,
								"cloud": tftypes.Object{
									AttributeTypes: map[string]tftypes.Type{
										"token":   tftypes.String,
										"url":     tftypes.String,
										"project": tftypes.String,
									},
								},
								"remote_dir": tftypes.Object{
									AttributeTypes: map[string]tftypes.Type{
										"name": tftypes.String,
										"tag":  tftypes.String,
									},
								},
								"env_name": tftypes.String,
								"status": tftypes.Object{
									AttributeTypes: map[string]tftypes.Type{
										"status":  tftypes.String,
										"current": tftypes.String,
										"next":    tftypes.String,
										"latest":  tftypes.String,
									},
								},
								"id": tftypes.String,
							},
						}, map[string]tftypes.Value{
							"url":              tftypes.NewValue(tftypes.String, "sqlite://:memory:"),
							"dev_url":          tftypes.NewValue(tftypes.String, "sqlite://:memory:"),
							"dir":              tftypes.NewValue(tftypes.String, "migrations?format=atlas"),
							"revisions_schema": tftypes.NewValue(tftypes.String, "atlas_schema_revisions"),
							"version":          tftypes.NewValue(tftypes.String, "20221101163823"),
							"baseline":         tftypes.NewValue(tftypes.String, "20221101163823"),
							"exec_order":       tftypes.NewValue(tftypes.String, "linear"),
							"cloud": tftypes.NewValue(tftypes.Object{
								AttributeTypes: map[string]tftypes.Type{
									"token":   tftypes.String,
									"url":     tftypes.String,
									"project": tftypes.String,
								},
							}, map[string]tftypes.Value{
								"token":   tftypes.NewValue(tftypes.String, "aci_bearer_token"),
								"url":     tftypes.NewValue(tftypes.String, "atlas"),
								"project": tftypes.NewValue(tftypes.String, "test"),
							}),
							"remote_dir": tftypes.NewValue(tftypes.Object{
								AttributeTypes: map[string]tftypes.Type{
									"name": tftypes.String,
									"tag":  tftypes.String,
								},
							}, map[string]tftypes.Value{
								"name": tftypes.NewValue(tftypes.String, "test"),
								"tag":  tftypes.NewValue(tftypes.String, "test"),
							}),
							"env_name": tftypes.NewValue(tftypes.String, "test"),
							"status": tftypes.NewValue(tftypes.Object{
								AttributeTypes: map[string]tftypes.Type{
									"status":  tftypes.String,
									"current": tftypes.String,
									"next":    tftypes.String,
									"latest":  tftypes.String,
								},
							}, map[string]tftypes.Value{
								"status":  tftypes.NewValue(tftypes.String, "PENDING"),
								"current": tftypes.NewValue(tftypes.String, "20221101163823"),
								"next":    tftypes.NewValue(tftypes.String, "20221101163841"),
								"latest":  tftypes.NewValue(tftypes.String, "20221101163841"),
							}),
							"id": tftypes.NewValue(tftypes.String, "test"),
						}),
						Schema: sdkschema.Schema{
							Attributes: map[string]sdkschema.Attribute{

								"url": sdkschema.StringAttribute{
									Optional: true,
								},
								"env_name": sdkschema.StringAttribute{
									Optional: true,
								},
								"status": sdkschema.ObjectAttribute{
									Optional: true,
									AttributeTypes: map[string]attr.Type{
										"status":  types.StringType,
										"current": types.StringType,
										"next":    types.StringType,
										"latest":  types.StringType,
									},
								},
								"dev_url": sdkschema.StringAttribute{
									Optional: true,
								},
								"id": sdkschema.StringAttribute{
									Optional: true,
								},
								"dir": sdkschema.StringAttribute{
									Optional: true,
								},
								"revisions_schema": sdkschema.StringAttribute{
									Optional: true,
								},
								"version": sdkschema.StringAttribute{
									Optional: true,
								},
								"remote_dir": sdkschema.SingleNestedAttribute{
									Optional: true,
									Attributes: map[string]sdkschema.Attribute{
										"name": sdkschema.StringAttribute{
											Optional: true,
										},
										"tag": sdkschema.StringAttribute{
											Optional: true,
										},
									},
								},
								"exec_order": sdkschema.StringAttribute{
									Optional: true,
								},
								"cloud": sdkschema.SingleNestedAttribute{
									Optional: true,
									Attributes: map[string]sdkschema.Attribute{
										"token": sdkschema.StringAttribute{
											Optional: true,
										},
										"url": sdkschema.StringAttribute{
											Optional: true,
										},
										"project": sdkschema.StringAttribute{
											Optional: true,
										},
									},
								},
							},
						},
					},
					State: tfsdk.State{},
				},
				resp: rs.ModifyPlanResponse{},
			},
		},
	}
	// c, err := atlas.NewClient(t.TempDir(), "atlas")
	// require.NoError(t, err)
	providerResp := sdkprovider.ConfigureResponse{}
	p := provider.New("", "", "")()
	p.Configure(context.Background(), sdkprovider.ConfigureRequest{
		Config: tfsdk.Config{
			Raw: tftypes.NewValue(
				tftypes.Object{
					AttributeTypes: map[string]tftypes.Type{
						"binary_path": tftypes.String,
						"dev_url":     tftypes.String,
						"cloud":       tftypes.Object{AttributeTypes: map[string]tftypes.Type{}},
					},
				},
				map[string]tftypes.Value{
					"binary_path": tftypes.NewValue(tftypes.String, "atlas"),
					"dev_url":     tftypes.NewValue(tftypes.String, "sqlite://:memory:"),
					"cloud":       tftypes.NewValue(tftypes.Object{AttributeTypes: map[string]tftypes.Type{}}, nil),
				},
			),
			Schema: sdkschema.Schema{
				Attributes: map[string]sdkschema.Attribute{
					"binary_path": sdkschema.StringAttribute{
						Optional: true,
					},
					"dev_url": sdkschema.StringAttribute{
						Optional: true,
					},
					"cloud": sdkschema.SingleNestedAttribute{
						Optional: true,
						Attributes: map[string]sdkschema.Attribute{
							"token": sdkschema.StringAttribute{
								Optional: true,
							},
							"url": sdkschema.StringAttribute{
								Optional: true,
							},
							"project": sdkschema.StringAttribute{
								Optional: true,
							},
						},
					},
				},
			},
		},
	}, &providerResp)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			z := p.Resources(context.Background())
			require.Len(t, z, 2)
			mgr, ok := z[1]().(*provider.MigrationResource)
			require.True(t, ok, "resource is not MigrationResource")
			mgr.Configure(context.Background(), rs.ConfigureRequest{
				ProviderData: providerResp.ResourceData,
			}, &rs.ConfigureResponse{})
			mresp := rs.ModifyPlanResponse{}
			mgr.ModifyPlan(context.Background(), tt.args.req, &mresp)
			warningsCount := mresp.Diagnostics.WarningsCount()
			require.Equal(t, 3, warningsCount)
		})
	}
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
