data "atlas_migration" "hello" {
  dir = "migrations?format=atlas"
  url = "mysql://root:pass@localhost:3307/hello"
}

resource "atlas_migration" "hello" {
  dir     = "migrations?format=atlas"
  version = data.atlas_migration.hello.latest # Use latest to run all migrations
  url     = data.atlas_migration.hello.url
}
