schema "dbo" {}

table "users" {
  schema = schema.dbo
  column "id" {
    null = false
    type = nvarchar(50)
  }
}