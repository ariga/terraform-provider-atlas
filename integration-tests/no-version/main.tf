terraform {
  required_providers {
    atlas = {
      source  = "ariga/atlas"
      version = "0.0.0-pre.0"
    }
  }
}

data "atlas_migration" "db" {
  dir = "./migrations"
  url = "sqlite://file.db"
}

resource "atlas_migration" "db" {
  dir     = "./migrations"
  url     = "sqlite://file.db"
  dev_url = "sqlite://file?mode=memory"
}
