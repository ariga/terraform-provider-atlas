provider "atlas" {
  # User MySQL 8 docker image as the dev database.
  dev_url = "docker://mysql/8/market"
}

data "atlas_schema" "market" {
  src     = file("${path.module}/schema.hcl")
}

resource "atlas_schema" "market" {
  hcl = data.atlas_schema.market.hcl
  url = "mysql://root:pass@localhost:3306/market"
}
