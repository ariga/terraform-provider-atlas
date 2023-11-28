# Atlas Terraform Provider

[![Discord](https://img.shields.io/discord/930720389120794674?label=discord&logo=discord&style=flat-square&logoColor=white)](https://discord.gg/zZ6sWVg6NT)

<a href="https://atlasgo.io">
  <img width="50%" align="right" style="display: block; margin:40px auto;" src="https://atlasgo.io/uploads/images/gopher.png"/>
</a>

Atlas tools help developers manage their database schemas by applying modern DevOps principles.
Contrary to existing tools, Atlas intelligently plans schema migrations for you, based on your desired state.

### Supported databases: 
* MySQL
* MariaDB
* PostgresSQL
* SQLite
* TiDB
* CockroachDB

### Docs
* [Provider Docs](https://registry.terraform.io/providers/ariga/atlas/latest/docs)
* [Atlas Docs](https://atlasgo.io)

## Installation

```terraform
terraform {
  required_providers {
    atlas = {
      source  = "ariga/atlas"
      version = "~> 0.6.1"
    }
  }
}
provider "atlas" {
  # Use MySQL 8 docker image as the dev database.
  dev_url = "docker://mysql/8"
}
```

## Quick Start

1\. To create a schema for your database, [first install `atlas`](https://atlasgo.io/getting-started#installation)

2\. Then, inspect the schema of the database:
```shell
atlas schema inspect -d "mysql://root:pass@localhost:3306/example" > schema.hcl
```

3\. Finally, configure the terraform resource to apply the state to your database:

```terraform
data "atlas_schema" "my_schema" {
  src     = "file://${abspath("./schema.hcl")}"
  dev_url = "mysql://root:pass@localhost:3307/example"
}

resource "atlas_schema" "example_db" {
  hcl     = data.atlas_schema.my_schema.hcl
  url     = "mysql://root:pass@localhost:3306/example"
  dev_url = "mysql://root:pass@localhost:3307/example"
}
```

For more advanced examples, check out the examples folder.
