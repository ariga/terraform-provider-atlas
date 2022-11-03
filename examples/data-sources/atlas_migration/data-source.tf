data "atlas_migration" "hello" {
  dir = "migrations?format=atlas"
  url = "mysql://root:pass@localhost:3307/hello"
}
