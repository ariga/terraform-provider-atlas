atlas {
  cloud {
    token = "token"
  }
}
data "remote_dir" "this" {
  name = "tf-dir"
  tag  = "tag"
}
env {
  name = atlas.env
  url  = "mysql://user:pass@localhost:3306/tf-db"
  migration {
    dir = data.remote_dir.this.url
  }
}
