schema "public" {}

table "users" {
  schema = schema.public
  column "id" {
    null = false
    type = bigint
  }
}