
provider "atlas" {}

data "atlas_schema" "at_schema" {
  depends_on = [ docker_container.dev ]
  dev_db_url = "mysql://root:pass@tcp(localhost:3307)/test"
  hcl = file("${path.module}/schema.hcl")
}

resource "atlas_schema" "mydb" {
  depends_on = [ docker_container.prod ]
  hcl = data.atlas_schema.at_schema.normal_hcl
  url = "mysql://root:pass@tcp(localhost:3306)/test"  
}
