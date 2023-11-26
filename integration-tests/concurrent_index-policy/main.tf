terraform {
  required_providers {
    atlas = {
      source  = "ariga/atlas"
      version = "0.0.0-pre.0"
    }
  }
}

variable "schema" {
  type    = string
  default = "schema-1.hcl"
}

data "atlas_schema" "db" {
  src     = "file://${var.schema}"
  dev_url = "postgres://postgres:pass@localhost:5433/"
}

resource "atlas_schema" "db" {
  hcl     = data.atlas_schema.db.hcl
  url     = "postgres://postgres:pass@localhost:5432/"
  dev_url = "postgres://postgres:pass@localhost:5433/"
  diff {
    concurrent_index {
      create = true
      drop   = true
    }
  }
}
