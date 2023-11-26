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
		data templateData
	}{
		{name: "token", data: templateData{
			URL: "mysql://user:pass@localhost:3306/tf-db",
			Cloud: &cloudConfig{
				Token: "token+%=_-",
			},
		}},
		{name: "cloud", data: templateData{
			URL: "mysql://user:pass@localhost:3306/tf-db",
			Cloud: &cloudConfig{
				Token:   "token",
				URL:     ptr("url"),
				Project: ptr("project"),
			},
			RemoteDir: &remoteDir{
				Name: "tf-dir",
			},
		}},
		{name: "local", data: templateData{
			URL: "mysql://user:pass@localhost:3306/tf-db",
		}},
		{name: "baseline", data: templateData{
			URL:      "mysql://user:pass@localhost:3306/tf-db",
			Baseline: "100000",
		}},
		{name: "cloud-no-token", data: templateData{
			URL: "mysql://user:pass@localhost:3306/tf-db",
			RemoteDir: &remoteDir{
				Name: "tf-dir",
			},
			DirURL: ptr("dir-url"),
		}},
		{name: "cloud-tag", data: templateData{
			URL: "mysql://user:pass@localhost:3306/tf-db",
			Cloud: &cloudConfig{
				Token: "token",
			},
			RemoteDir: &remoteDir{
				Name: "tf-dir",
				Tag:  ptr("tag"),
			},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name := filepath.Join(t.TempDir(), "atlas.hcl")
			require.NoError(t, tt.data.CreateFile(name))
			checkContent(t, name, func(s string) error {
				if !update {
					return nil
				}
				return tt.data.CreateFile(s)
			})
		})
	}
}

func ptr(s string) *string {
	return &s
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

func Test_SchemaTemplate(t *testing.T) {
	data := &schemaData{
		Source: "file://schema.hcl",
		URL:    "mysql://user:pass@localhost:3306/tf-db",
		DevURL: "mysql://user:pass@localhost:3307/tf-db",
		Diff: &Diff{
			Skip: &SkipChanges{
				AddIndex:  true,
				DropTable: true,
			},
		},
	}

	out := &bytes.Buffer{}
	require.NoError(t, data.render(out))
	require.Equal(t, `
diff {
  skip {
    drop_table = true
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
