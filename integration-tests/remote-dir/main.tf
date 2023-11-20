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

data "atlas_migration" "db" {
  url = "sqlite://file.db"
  remote_dir {
    name = "tf-remote-dir"
  }
}

resource "atlas_migration" "db" {
  url = "sqlite://file.db"
  remote_dir {
    name = "tf-remote-dir"
  }
}
