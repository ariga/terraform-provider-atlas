env "tf" {
  url = "mysql://user:pass@localhost:3306/tf-db"
  migration {
    dir = "atlas://tf-dir?tag=latest"
  }
}
