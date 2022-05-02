data "atlas_schema" "norm" {
  // dev_db_url is used for normalization, see: https://atlasgo.io/cli/dev-database.
  dev_db_url = "mysql://root:pass@localhost:3307/test"
  src = file("${path.module}/human_schema.hcl")
  // will compute `hcl`
}