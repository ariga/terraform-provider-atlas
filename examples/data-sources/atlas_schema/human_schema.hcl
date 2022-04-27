// This example show how an `atlas_schema` data source normalizes a human hcl file into machine normalized one.
table "table" {
  schema = schema.test
  column "id" {
    null           = false
    type           = int
    auto_increment = true
  }

  column "name" {
    null = false
    type = varchar(20)
  }
  primary_key {
    columns = [column.id]
  }
}
schema "test" {
  charset = "latin1"
  collate = "latin1_swedish_ci"
}
