
env {
  name = atlas.env
  url  = "mysql://user:pass@localhost:3306/tf-db"
  migration {
    dir = "file://migrations"
    exec_order = LINEAR_SKIP
  }
}
