table "orders" {
  schema = schema.market
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

schema "market" {
  charset = "utf8mb4"
  collate = "utf8mb4_0900_ai_ci"
}
