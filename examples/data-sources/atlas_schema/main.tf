data "atlas_schema" "norm" {
  // dev_db_url is used for normalization, see: https://atlasgo.io/cli/dev-database.
  dev_db_url = "mysql://root:pass@tcp(localhost:3307)/test"
  hcl = file("${path.module}/human_schema.hcl")
  // will compute `normal_hcl`
}