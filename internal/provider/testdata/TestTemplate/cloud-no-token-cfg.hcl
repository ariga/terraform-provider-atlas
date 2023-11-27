
data "remote_dir" "this" {
  name = "tf-dir"
}
env {
  name = atlas.env
  url  = "mysql://user:pass@localhost:3306/tf-db"
  migration {
    dir = "file://dir-url"
  }
}
