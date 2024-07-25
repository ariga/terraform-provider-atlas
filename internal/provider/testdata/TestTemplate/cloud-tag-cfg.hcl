env {
  name = atlas.env
  url  = "mysql://user:pass@localhost:3306/tf-db"
  migration {
    dir = "atlas://tf-dir?tag=tag"
  }
}
atlas {
  cloud {
    token = "token"
  }
}
