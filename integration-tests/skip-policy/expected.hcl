table "t1" {
  schema = schema.test
  column "c1" {
    null = false
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
    null = false
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
  charset = "utf8mb4"
  collate = "utf8mb4_0900_ai_ci"
}
