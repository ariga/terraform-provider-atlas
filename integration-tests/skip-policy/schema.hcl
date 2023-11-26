table "t1" {
  schema = schema.test
  column "c1" {
    type = int
  }
  column "c2" {
    null = true
    type = text
  }
  primary_key {
    columns = [column.c1]
  }
}
table "t2" {
  schema = schema.test
  column "c1" {
    type = int
  }
  column "c2" {
    null = true
    type = text
  }
  primary_key {
    columns = [column.c1]
  }
}
schema "test" {
}