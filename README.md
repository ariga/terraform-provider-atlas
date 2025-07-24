# Atlas Terraform Provider

[![Twitter](https://img.shields.io/twitter/url.svg?label=Follow%20%40ariga%2Fatlas&style=social&url=https%3A%2F%2Ftwitter.com%2Fatlasgo_io)](https://twitter.com/atlasgo_io)
[![Discord](https://img.shields.io/discord/930720389120794674?label=discord&logo=discord&style=flat-square&logoColor=white)](https://discord.com/invite/zZ6sWVg6NT)

<p>
  <a href="https://atlasgo.io" target="_blank">
  <img alt="image" src="https://github.com/ariga/atlas/assets/7413593/2e27cb81-bad6-491a-8d9c-20920995a186">
  </a>
</p>

Atlas is a language-agnostic tool for managing and migrating database schemas using modern DevOps principles.
It offers two workflows:

- **Declarative**: Similar to Terraform, Atlas compares the current state of the database to the desired state, as
  defined in an [HCL], [SQL], or [ORM] schema. Based on this comparison, it generates and executes a migration plan to
  transition the database to its desired state.

- **Versioned**: Unlike other tools, Atlas automatically plans schema migrations for you. Users can describe their desired
  database schema in [HCL], [SQL], or their chosen [ORM], and by utilizing Atlas, they can plan, lint, and apply the
  necessary migrations to the database.

## Installation

```terraform
terraform {
  required_providers {
    atlas = {
      source  = "ariga/atlas"
      version = "~> 0.9.8"
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
atlas schema inspect -u "mysql://root:pass@localhost:3306/example" > schema.hcl
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

### Docs

- [Provider Docs](https://registry.terraform.io/providers/ariga/atlas/latest/docs)
- [Atlas Docs](https://atlasgo.io)

### Supported databases:

MySQL, MariaDB, PostgresSQL, SQLite, TiDB, CockroachDB, SQL Server, ClickHouse, Redshift.

[HCL]: https://atlasgo.io/atlas-schema/hcl
[SQL]: https://atlasgo.io/atlas-schema/sql
[ORM]: https://atlasgo.io/atlas-schema/external
