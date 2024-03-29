data "atlas_schema" "market" {
  dev_url = "mysql://root:pass@localhost:3307/test"
  src     = file("${path.module}/schema.hcl")
}

resource "atlas_schema" "market" {
  hcl = data.atlas_schema.market.hcl
  url = "mysql://root:pass@localhost:3306/test"
}
