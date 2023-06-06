package provider_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/stretchr/testify/require"

	"ariga.io/ariga/terraform-provider-atlas/internal/atlas"
	"ariga.io/ariga/terraform-provider-atlas/internal/provider"
)

const (
	testAccData = `variable "tenant" {
		type = string
	}
	schema "test" {
		name    = var.tenant
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
	}`
	normalHCL = `table "foo" {
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
schema "test" {
  charset = "utf8mb4"
  collate = "utf8mb4_0900_ai_ci"
}
`
)

func TestAccSchemaDataSource(t *testing.T) {
	tempSchemas(t, mysqlDevURL, "test")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Read testing
			{
				Config: fmt.Sprintf(`data "atlas_schema" "market" {
					dev_db_url = "mysql://root:pass@localhost:3307/test"
					variables = [
						"tenant=test",
					]
					src        = <<-EOT
					%s
					EOT
				}`, testAccData),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.atlas_schema.market", "hcl", normalHCL),
					resource.TestCheckResourceAttr("data.atlas_schema.market", "id", "/WWD4tjYzwMDMHxlNwuhrg"),
				),
			},
		},
	})
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Read testing
			{
				Config: `data "atlas_schema" "market" {
					dev_db_url = "mysql://root:pass@localhost:3307/test"
					src = ""
				}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("data.atlas_schema.market", "hcl"),
					resource.TestCheckResourceAttr("data.atlas_schema.market", "id", "bGInLge7AUJiuCF1YpXFjQ"),
				),
			},
		},
	})

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Read testing
			{
				Config: `data "atlas_schema" "market" {
					dev_url = "mysql://root:pass@localhost:3307/test"
					src     = "file://./sql-files/schema.sql"
				}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.atlas_schema.market", "hcl", normalHCL),
					resource.TestCheckResourceAttr("data.atlas_schema.market", "id", "/WWD4tjYzwMDMHxlNwuhrg"),
				),
			},
		},
	})
	// Use DevDB from provider config
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Read testing
			{
				Config: `
				provider "atlas" {
					dev_url = "mysql://root:pass@localhost:3307/test"
				}
				data "atlas_schema" "hello" {
					src = "file://./sql-files/schema.sql"
				}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.atlas_schema.hello", "hcl", normalHCL),
					resource.TestCheckResourceAttr("data.atlas_schema.hello", "id", "/WWD4tjYzwMDMHxlNwuhrg"),
				),
			},
		},
	})
}

func TestParseVariablesToHCL(t *testing.T) {
	type args struct {
		ctx  context.Context
		data types.List
	}
	tests := []struct {
		name      string
		args      args
		wantVars  atlas.Vars
		wantDiags diag.Diagnostics
	}{
		{
			name: "happy-case",
			args: args{
				ctx: context.Background(),
				data: listStrings(
					"foo=bar",
					"bar=true",
					"num1=1",
					"num1=2",
					"num2=2",
				),
			},
			wantVars: atlas.Vars{
				"foo":  []string{"bar"},
				"bar":  []string{"true"},
				"num1": []string{"1", "2"},
				"num2": []string{"2"},
			},
			wantDiags: nil,
		},
		{
			name: "invalid",
			args: args{
				ctx: context.Background(),
				data: listStrings(
					"errr",
				),
			},
			wantVars: atlas.Vars{},
			wantDiags: diag.Diagnostics{
				diag.NewErrorDiagnostic("Variables Error",
					"Unable to parse variables, got error: variables must be format as key=value, got: \"errr\""),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := make(atlas.Vars)
			digs := provider.ParseVariablesToVars(tt.args.ctx, tt.args.data, v)
			require.Equal(t, tt.wantDiags, digs)
			require.Equal(t, tt.wantVars, v)
		})
	}
}

func listStrings(s ...string) types.List {
	elems := make([]attr.Value, len(s))
	for i, v := range s {
		elems[i] = types.String{Value: v}
	}
	return types.List{
		Elems:    elems,
		ElemType: types.StringType,
	}
}
