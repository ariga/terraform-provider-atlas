env {
  name = atlas.env
  url  = "mysql://user:pass@localhost:3306/tf-db"
  migration {
    dir = "atlas://tf-dir?tag=latest"
  }
}
atlas {
  cloud {
    token   = "token"
    project = "project"
    url     = "url"
  }
}
