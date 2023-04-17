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
