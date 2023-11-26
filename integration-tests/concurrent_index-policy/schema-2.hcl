schema "public" {}

table "users" {
  schema = schema.public
  column "id" {
    null = false
    type = bigint
  }
  index "users_idx" {
    columns = [column.id]
  }
}