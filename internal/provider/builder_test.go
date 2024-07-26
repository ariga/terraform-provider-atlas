package provider

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTemplate(t *testing.T) {
	var update = false
	tests := []struct {
		name string
		data projectConfig
	}{
		{name: "token", data: projectConfig{
			Config: baseAtlasHCL,
			Cloud: &cloudConfig{
				Token: "token+%=_-",
			},
			Env: &envConfig{
				URL: "mysql://user:pass@localhost:3306/tf-db",
				Migration: &migrationConfig{
					DirURL: "file://migrations",
				},
			},
		}},
		{name: "cloud", data: projectConfig{
			Config: baseAtlasHCL,
			Cloud: &cloudConfig{
				Token:   "token",
				URL:     ptr("url"),
				Project: ptr("project"),
			},
			Env: &envConfig{
				URL: "mysql://user:pass@localhost:3306/tf-db",
				Migration: &migrationConfig{
					DirURL: "atlas://tf-dir?tag=latest",
				},
			},
		}},
		{name: "local", data: projectConfig{
			Config: baseAtlasHCL,
			Env: &envConfig{
				URL: "mysql://user:pass@localhost:3306/tf-db",
				Migration: &migrationConfig{
					DirURL: "file://migrations",
				},
			},
		}},
		{name: "local-exec-order", data: projectConfig{
			Config: baseAtlasHCL,
			Env: &envConfig{
				URL: "mysql://user:pass@localhost:3306/tf-db",
				Migration: &migrationConfig{
					DirURL:    "file://migrations",
					ExecOrder: "linear-skip",
				},
			},
		}},
		{name: "baseline", data: projectConfig{
			Config: baseAtlasHCL,
			Env: &envConfig{
				URL: "mysql://user:pass@localhost:3306/tf-db",
				Migration: &migrationConfig{
					DirURL:   "file://migrations",
					Baseline: "100000",
				},
			},
		}},
		{name: "cloud-tag", data: projectConfig{
			Config: baseAtlasHCL,
			Cloud: &cloudConfig{
				Token: "token",
			},
			Env: &envConfig{
				URL: "mysql://user:pass@localhost:3306/tf-db",
				Migration: &migrationConfig{
					DirURL: "atlas://tf-dir?tag=tag",
				},
			},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			require.NoError(t, tt.data.Render(buf))
			checkContent(t, buf.String(), func(s string) error {
				if !update {
					return nil
				}
				f, err := os.Create(s)
				if err != nil {
					return err
				}
				defer f.Close()
				_, err = io.Copy(f, buf)
				return err
			})
		})
	}
}

func Test_SchemaTemplate(t *testing.T) {
	data := &projectConfig{
		Config: baseAtlasHCL,
		Env: &envConfig{
			Source: "file://schema.hcl",
			URL:    "mysql://user:pass@localhost:3306/tf-db",
			DevURL: "mysql://user:pass@localhost:3307/tf-db",
			Diff: &Diff{
				ConcurrentIndex: &ConcurrentIndex{
					Create: ptr(true),
				},
				Skip: &SkipChanges{
					AddIndex:  ptr(true),
					DropTable: ptr(false),
				},
			},
		},
	}

	out := &bytes.Buffer{}
	require.NoError(t, data.Render(out))
	require.Equal(t, `env {
  dev  = "mysql://user:pass@localhost:3307/tf-db"
  name = atlas.env
  src  = "file://schema.hcl"
  url  = "mysql://user:pass@localhost:3306/tf-db"
  diff {
    concurrent_index {
      create = true
    }
    skip {
      drop_table = false
      add_index  = true
    }
  }
}`, out.String())
}

func Test_mergeFile(t *testing.T) {
	dst, err := parseConfig(`
atlas {}
env {
  name = atlas.env
}
`)
	require.NoError(t, err)

	src, err := parseConfig(`
atlas {
	cloud {
  	token = "aci_token"
  }
}
env {
  url = "sqlite://file.db"
  dev = "sqlite://file?mode=memory"
  migration {
		dir = "file://migrations"
	}
}
`)
	require.NoError(t, err)
	mergeFile(dst, src)

	require.Equal(t, `
atlas {
  cloud {
    token = "aci_token"
  }
}
env {
  name = atlas.env
  dev  = "sqlite://file?mode=memory"
  url  = "sqlite://file.db"
  migration {
    dir = "file://migrations"
  }
}
`, string(dst.Bytes()))
}

func checkContent(t *testing.T, actual string, gen func(string) error) {
	t.Helper()
	expected := filepath.Join(".", "testdata", fmt.Sprintf("%s-cfg.hcl", t.Name()))
	require.NoError(t, gen(expected))
	require.FileExists(t, expected)
	e, err := os.ReadFile(expected)
	require.NoError(t, err)
	require.Equal(t, string(e), actual)
}

func ptr[T any](s T) *T {
	return &s
}
