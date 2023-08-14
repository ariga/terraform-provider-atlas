

env {
  name = atlas.env
  url  = "mysql://user:pass@localhost:3306/tf-db"
  migration {
    dir = "file://migrations"
    baseline = "100000"
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
