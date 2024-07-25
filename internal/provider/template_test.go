package provider

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTemplate(t *testing.T) {
	var update = false
	tests := []struct {
		name string
		data atlasHCL
	}{
		{name: "token", data: atlasHCL{
			URL: "mysql://user:pass@localhost:3306/tf-db",
			Cloud: &cloudConfig{
				Token: "token+%=_-",
			},
			Migration: &migrationConfig{
				DirURL: "file://migrations",
			},
		}},
		{name: "cloud", data: atlasHCL{
			URL: "mysql://user:pass@localhost:3306/tf-db",
			Cloud: &cloudConfig{
				Token:   "token",
				URL:     ptr("url"),
				Project: ptr("project"),
			},
			Migration: &migrationConfig{
				DirURL: "atlas://tf-dir?tag=latest",
			},
		}},
		{name: "local", data: atlasHCL{
			URL: "mysql://user:pass@localhost:3306/tf-db",
			Migration: &migrationConfig{
				DirURL: "file://migrations",
			},
		}},
		{name: "local-exec-order", data: atlasHCL{
			URL: "mysql://user:pass@localhost:3306/tf-db",
			Migration: &migrationConfig{
				DirURL:    "file://migrations",
				ExecOrder: "linear-skip",
			},
		}},
		{name: "baseline", data: atlasHCL{
			URL: "mysql://user:pass@localhost:3306/tf-db",
			Migration: &migrationConfig{
				DirURL:   "file://migrations",
				Baseline: "100000",
			},
		}},
		{name: "cloud-tag", data: atlasHCL{
			URL: "mysql://user:pass@localhost:3306/tf-db",
			Cloud: &cloudConfig{
				Token: "token",
			},
			Migration: &migrationConfig{
				DirURL: "atlas://tf-dir?tag=tag",
			},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name := filepath.Join(t.TempDir(), "atlas.hcl")
			require.NoError(t, tt.data.CreateFile(migrationAtlasHCL, name))
			checkContent(t, name, func(s string) error {
				if !update {
					return nil
				}
				return tt.data.CreateFile(migrationAtlasHCL, s)
			})
		})
	}
}

func Test_SchemaTemplate(t *testing.T) {
	data := &schemaData{
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
	}

	out := &bytes.Buffer{}
	require.NoError(t, data.render(out))
	require.Equal(t, `
diff {
  concurrent_index {
    create = true
  }
  skip {
    drop_table = false
    add_index = true
  }
}
env {
  name = atlas.env
  src  = "file://schema.hcl"
  url  = "mysql://user:pass@localhost:3306/tf-db"
  dev  = "mysql://user:pass@localhost:3307/tf-db"
  schemas = []
  exclude = []
}
`, out.String())
}

func checkContent(t *testing.T, actual string, gen func(string) error) {
	t.Helper()
	expected := filepath.Join(".", "testdata", fmt.Sprintf("%s-cfg.hcl", t.Name()))
	require.NoError(t, gen(expected))
	require.FileExists(t, expected)
	require.FileExists(t, actual)
	e, err := os.ReadFile(expected)
	require.NoError(t, err)
	a, err := os.ReadFile(actual)
	require.NoError(t, err)
	require.Equal(t, string(e), string(a))
}

func ptr[T any](s T) *T {
	return &s
}
