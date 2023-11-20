terraform {
  required_providers {
    atlas = {
      source  = "ariga/atlas"
      version = "0.0.0-pre.0"
    }
  }
}

data "atlas_schema" "db" {
  src     = "file://schema.sql"
  dev_url = "sqlite://file?mode=memory"
}

resource "atlas_schema" "db" {
  hcl     = data.atlas_schema.db.hcl
  url     = "sqlite://file.db"
  dev_url = "sqlite://file?mode=memory"
}
