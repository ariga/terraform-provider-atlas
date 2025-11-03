# Example: using data source to get the latest migration version
data "atlas_migration" "hello" {
  dir = "migrations?format=atlas"
  url = "mysql://root:pass@localhost:3307/hello"
}

resource "atlas_migration" "hello" {
  dir     = "migrations?format=atlas"
  version = data.atlas_migration.hello.latest # Use latest to run all migrations
  url     = data.atlas_migration.hello.url
}

# Example: using atlas project configuration to manage multiple environments
locals {
  # Define connection details for each environment
  environments = {
    staging = {
      url = "mysql://root:pass@localhost:3307/dev"
    }
    production = {
      url = "mysql://root:pass@localhost:3306/prod"
    }
  }
  # Atlas project configuration
  atlas_config = <<-EOT
    variable "url" {
      type = string
    }
    env "tf" {
      url = var.url
      dev_url = "docker://mysql/8"
      migration {
        dir    = "file://${abspath("${path.module}/migrations")}" # always use absolute path
        tx_mode = none
      }
    }
  EOT
}

resource "atlas_migration" "multi_env" {
  env_name = "tf"
  config   = local.atlas_config
  variables = jsonencode({
    url = local.environments[terraform.workspace].url
  })
}