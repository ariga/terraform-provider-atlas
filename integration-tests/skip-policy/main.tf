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
  default = "schema"
}

variable "skip_drop_table" {
  type    = bool
  default = false
}

data "atlas_schema" "db" {
  src     = "file://${var.schema}.hcl"
  dev_url = "mysql://root:pass@localhost:3307/"
}

resource "atlas_schema" "db" {
  hcl     = data.atlas_schema.db.hcl
  url     = "mysql://root:pass@localhost:3306/"
  dev_url = "mysql://root:pass@localhost:3307/"
  diff {
    skip {
      drop_table = var.skip_drop_table
    }
  }
}
