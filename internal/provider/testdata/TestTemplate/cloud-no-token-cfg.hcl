

data "remote_dir" "this" {
  name = "tf-dir"
  tag  = "latest"
}
env {
  name = atlas.env
  url  = "mysql://user:pass@localhost:3306/tf-db"
  migration {
    dir = "file://dir-url"
  }
  format {
    migrate {
      apply  = "{{ json . }}"
      lint   = "{{ json . }}"
      status = "{{ json . }}"
    }
  }
  lint {
    format = "{{ json . }}"
  }
}
