terraform {
  required_providers {
    atlas = {
      source  = "ariga/atlas"
      version = "0.0.0-pre.0"
    }
  }
}

# Data source with exclude: filters out t2 table from normalization
data "atlas_schema" "db" {
  src     = "file://schema.sql"
  dev_url = "sqlite://file?mode=memory"
  exclude = ["t2"]
}

# Resource with exclude: applies only t1, ignoring t2 on the target
resource "atlas_schema" "db" {
  hcl     = data.atlas_schema.db.hcl
  url     = "sqlite://file.db"
  dev_url = "sqlite://file?mode=memory"
  exclude = ["t2"]
}
