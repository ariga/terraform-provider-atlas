table "some_table" {
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
