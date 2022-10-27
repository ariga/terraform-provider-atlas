package provider_test

import (
	"context"
	"reflect"
	"testing"

	"ariga.io/ariga/terraform-provider-atlas/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/zclconf/go-cty/cty"
)

const testAccData = `
data "atlas_schema" "market" {
	dev_db_url = "mysql://root:pass@localhost:3307/test"
	variables = [
		"tenant: bar",
	]
	src = <<-EOT
	variable "tenant" {
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
	}
	EOT
}
`

const normalHCL = `table "foo" {
  schema = schema.bar
  column "id" {
    null           = false
    type           = int
    auto_increment = true
  }
  primary_key {
    columns = [column.id]
  }
}
schema "bar" {
  charset = "utf8mb4"
  collate = "utf8mb4_0900_ai_ci"
}
`

func TestAccSchemaDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Read testing
			{
				Config: testAccData,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.atlas_schema.market", "hcl", normalHCL),
					resource.TestCheckResourceAttr("data.atlas_schema.market", "id", "8muTzP+UOG/yYKva2oU1FA"),
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
}

func Test_parseVariablesToHCL(t *testing.T) {
	type args struct {
		ctx    context.Context
		data   types.List
		result map[string]cty.Value
	}
	tests := []struct {
		name       string
		args       args
		wantResult map[string]cty.Value
		wantDiags  diag.Diagnostics
	}{
		{
			args: args{
				ctx: context.Background(),
				data: types.List{
					Elems: []attr.Value{
						types.String{Value: "foo: bar"},
						types.String{Value: "bar: boo"},
					},
					ElemType: types.StringType,
				},
				result: map[string]cty.Value{},
			},
			wantResult: map[string]cty.Value{
				"foo": cty.StringVal("bar"),
				"bar": cty.StringVal("boo"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotDiags := provider.ParseVariablesToHCL(tt.args.ctx, tt.args.data, tt.args.result); !reflect.DeepEqual(gotDiags, tt.wantDiags) {
				t.Errorf("parseVariablesToHCL() = %v, want %v", gotDiags, tt.wantDiags)
			}
		})
	}
}
