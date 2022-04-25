data "atlas_schema" "at_schema" {
  dev_db_url = "mysql://root:pass@tcp(localhost:3307)/test"
  hcl = file("${path.module}/schema.hcl")
}

resource "atlas_schema" "mydb" {
  hcl = data.atlas_schema.at_schema.normal_hcl
  url = "mysql://root:pass@tcp(localhost:3306)/test"  
}
