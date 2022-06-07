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
      version = "~> 0.1.0"
    }
  }
}
provider "atlas" {}
```

## Quick Start

1\. To create a schema for your database, first install `atlas`:  
 ### MacOS:
 ```shell
 brew install ariga/tap/atlas
 ```
 ### Linux
 Download:
 ```shell
 curl -LO https://release.ariga.io/atlas/atlas-linux-amd64-latest
 ```
 Install:
 ```shell
 sudo install -o root -g root -m 0755 ./atlas-linux-amd64-latest /usr/local/bin/atlas
 ```
 ### Windows
 Download the [latest release](https://release.ariga.io/atlas/atlas-windows-amd64-latest.exe) and move the atlas binary to a file location on your system PATH.

2\. Then, inspect the schema of the database:
 ```shell
 atlas schema inspect -d "mysql://root:pass@localhost:3306/example" > schema.hcl
 ```
 
3\. Finally, configure the terraform resource to apply the state to your database:
 ```terraform
 data "atlas_schema" "my_schema" {
  src = file("${path.module}/schema.hcl")
  dev_db_url = "mysql://root:pass@localhost:3307/example"
 }

 resource "atlas_schema" "example_db" {
  hcl = data.atlas_schema.my_schema.hcl
  url = "mysql://root:pass@localhost:3306/example"
  dev_db_url = "mysql://root:pass@localhost:3307/example"
 }
 ```

For more advanced examples, check out the examples folder.
