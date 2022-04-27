<a href="https://atlasgo.io">
  <img title="Atlas" alt="Atlas logo" height="180" align="right" src="https://atlasgo.io/uploads/images/gopher.png"/>
</a>

# Atlas Terraform Provider

* Chat and Support: [discord](https://discord.gg/zZ6sWVg6NT)
* Supported Databases: SQLite, MySQL, TiDB, MariaDB, PostgreSQL.
<!-- * Documentation: link to terraform website -->

## Installation

```terraform
terraform {
  required_providers {
    atlas = {
      source  = "ariga/atlas"
      version = "0.0.1"
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
  dev_db_url = "mysql://root:pass@tcp(localhost:3307)/test"
 }

 resource "atlas_schema" "example_db" {
  hcl = data.atlas_schema.my_schema.hcl
  url = "mysql://root:pass@tcp(localhost:3306)/test"  
 }
 ```

For more advanced examples, check out the examples folder.
