schema "script_cli_migrate_diff_policy" {}

table "users" {
  schema = schema.script_cli_migrate_diff_policy
  column "id" {
    null = false
    type = bigint
  }
  index "users_idx" {
    columns = [column.id]
  }
}