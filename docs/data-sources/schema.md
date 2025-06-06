---
# generated by https://github.com/hashicorp/terraform-plugin-docs
page_title: "atlas_schema Data Source - terraform-provider-atlas"
subcategory: ""
description: |-
  atlas_schema data source uses dev-db to normalize the HCL schema in order to create better terraform diffs
---

# atlas_schema (Data Source)

atlas_schema data source uses dev-db to normalize the HCL schema in order to create better terraform diffs

## Example Usage

```terraform
data "atlas_schema" "norm" {
  src = file("${path.module}/human_schema.hcl")
  // dev_url is used for normalization, see: https://atlasgo.io/cli/dev-database.
  dev_url = "mysql://root:pass@localhost:3307/test"
  // will compute `hcl`
}

data "atlas_schema" "hello" {
  // use absolute path to avoid relative path issues
  src = "file://${abspath("./schema.sql")}"
  // dev_url is used for normalization, see: https://atlasgo.io/cli/dev-database.
  dev_url = "mysql://root:pass@localhost:3307/"
}

resource "atlas_schema" "hello" {
  url     = "mysql://root:pass@localhost:3306/"
  hcl     = data.atlas_schema.hello.hcl
  dev_url = "mysql://root:pass@localhost:3307/"
}
```

<!-- schema generated by tfplugindocs -->
## Schema

### Required

- `src` (String) The schema definition of the database. This attribute can be HCL schema or an URL to HCL/SQL file.

### Optional

- `cloud` (Block, Optional) (see [below for nested schema](#nestedblock--cloud))
- `dev_url` (String, Sensitive) The url of the dev-db see https://atlasgo.io/cli/url
- `variables` (Map of String) The map of variables used in the HCL.

### Read-Only

- `hcl` (String) The normalized form of the HCL
- `id` (String) The ID of this resource

<a id="nestedblock--cloud"></a>
### Nested Schema for `cloud`

Optional:

- `project` (String, Deprecated)
- `repo` (String)
- `token` (String)
- `url` (String)
