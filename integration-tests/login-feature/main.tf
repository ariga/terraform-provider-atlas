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
  dev_url = "sqlserver://sa:P@ssw0rd0995@localhost:1434?database=master"
}

resource "atlas_schema" "db" {
  hcl     = data.atlas_schema.db.hcl
  url     = "sqlserver://sa:P@ssw0rd0995@localhost:1433?database=master"
  dev_url = "sqlserver://sa:P@ssw0rd0995@localhost:1434?database=master"
}
