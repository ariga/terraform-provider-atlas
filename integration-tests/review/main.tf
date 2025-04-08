terraform {
  required_providers {
    atlas = {
      source  = "ariga/atlas"
      version = "0.0.0-pre.0"
    }
  }
}

variable "atlas_token" {
  type      = string
  sensitive = true
}

provider "atlas" {
  cloud {
    token = var.atlas_token
  }
}

resource "atlas_schema" "db" {
  hcl = <<-EOT
    table "t1" {
      schema = schema.main
      column "c1" {
        null = false
        type = int
      }
    }
    schema "main" {
    }
  EOT
  lint {
    review = "ALWAYS"
  }
  cloud {
    repo = "atlas-terraform"
  }
  url = "sqlite://example.db"
  dev_url = "sqlite://test.db?mode=memory"
}