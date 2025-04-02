package provider

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/require"
)

func TestTemplate(t *testing.T) {
	var update = false
	tests := []struct {
		name string
		data projectConfig
	}{
		{name: "token", data: projectConfig{
			EnvName: "tf",
			Env: &envConfig{
				URL: "mysql://user:pass@localhost:3306/tf-db",
				Migration: &migrationConfig{
					DirURL: "file://migrations",
				},
			},
		}},
		{name: "cloud", data: projectConfig{
			EnvName: "tf",
			Env: &envConfig{
				URL: "mysql://user:pass@localhost:3306/tf-db",
				Migration: &migrationConfig{
					DirURL: "atlas://tf-dir?tag=latest",
				},
			},
		}},
		{name: "local", data: projectConfig{
			EnvName: "tf",
			Env: &envConfig{
				URL: "mysql://user:pass@localhost:3306/tf-db",
				Migration: &migrationConfig{
					DirURL: "file://migrations",
				},
			},
		}},
		{name: "local-exec-order", data: projectConfig{
			EnvName: "tf",
			Env: &envConfig{
				URL: "mysql://user:pass@localhost:3306/tf-db",
				Migration: &migrationConfig{
					DirURL:    "file://migrations",
					ExecOrder: "linear-skip",
				},
			},
		}},
		{name: "baseline", data: projectConfig{
			EnvName: "tf",
			Env: &envConfig{
				URL: "mysql://user:pass@localhost:3306/tf-db",
				Migration: &migrationConfig{
					DirURL:   "file://migrations",
					Baseline: "100000",
				},
			},
		}},
		{name: "cloud-tag", data: projectConfig{
			EnvName: "tf",
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
	tests := []struct {
		name     string
		data     *projectConfig
		expected string
	}{
		{
			name: "empty env",
			data: &projectConfig{
				Config:  "",
				EnvName: "tf",
				Env:     &envConfig{},
			},
			expected: `env "tf" {
}
`,
		},
		{
			name: "default",
			data: &projectConfig{
				Config:  "",
				EnvName: "tf",
				Env: &envConfig{
					Source: "file://schema.hcl",
					URL:    "mysql://user:pass@localhost:3306/tf-db",
					DevURL: "mysql://user:pass@localhost:3307/tf-db",
					Diff: &Diff{
						ConcurrentIndex: &ConcurrentIndex{
							Create: types.BoolValue(true),
						},
						Skip: &SkipChanges{
							AddIndex:  types.BoolValue(true),
							DropTable: types.BoolValue(false),
						},
					},
					Lint: &Lint{
						Review: types.StringValue("ALWAYS"),
					},
				},
			},
			expected: `env "tf" {
  dev = "mysql://user:pass@localhost:3307/tf-db"
  src = "file://schema.hcl"
  url = "mysql://user:pass@localhost:3306/tf-db"
  diff {
    concurrent_index {
      create = true
    }
    skip {
      drop_table = false
      add_index  = true
    }
  }
  lint {
    review = "ALWAYS"
  }
}
`,
		},
		{
			name: "migration-repo",
			data: &projectConfig{
				Config:  "",
				EnvName: "tf",
				Env: &envConfig{
					Source: "file://schema.hcl",
					URL:    "mysql://user:pass@localhost:3306/tf-db",
					Migration: &migrationConfig{
						Repo: "test",
					},
				},
			},
			expected: `env "tf" {
  src = "file://schema.hcl"
  url = "mysql://user:pass@localhost:3306/tf-db"
  migration {
    repo = "test"
  }
}
`,
		},
		{
			name: "schema-repo",
			data: &projectConfig{
				Config:  "",
				EnvName: "tf",
				Env: &envConfig{
					Source: "file://schema.hcl",
					URL:    "mysql://user:pass@localhost:3306/tf-db",
					Schema: &schemaConfig{
						Repo: "test",
					},
				},
			},
			expected: `env "tf" {
  src = "file://schema.hcl"
  url = "mysql://user:pass@localhost:3306/tf-db"
  schema {
    repo = "test"
  }
}
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := &bytes.Buffer{}
			require.NoError(t, tt.data.Render(out))
			require.Equal(t, tt.expected, out.String())
		})
	}
}

func Test_mergeEnv(t *testing.T) {
	envBlock := (&envConfig{
		URL:    "sqlite://file.db",
		DevURL: "sqlite://file?mode=memory",
		Migration: &migrationConfig{
			DirURL: "file://migrations",
		},
	}).AsBlock()

	// Merge with existing env block.
	dst, err := parseConfig(`
env "foo" {
}
`)
	require.NoError(t, err)
	require.NoError(t, mergeEnvBlock(dst.Body(), envBlock, "foo"))
	require.Equal(t, `
env "foo" {
  dev = "sqlite://file?mode=memory"
  url = "sqlite://file.db"
  migration {
    dir = "file://migrations"
  }
}
`, string(dst.Bytes()))

	// Merge with non-existing env block.
	dst, err = parseConfig(`
env "bar" {
}
`)
	require.NoError(t, err)
	require.ErrorContains(t, mergeEnvBlock(dst.Body(), envBlock, "foo"), `the env block "foo" was not found in the give config`)

	// Merge with un-named env block.
	dst, err = parseConfig(`
env {
  name = atlas.env
}
`)
	require.NoError(t, err)
	require.NoError(t, mergeEnvBlock(dst.Body(), envBlock, "foo"))
	require.Equal(t, `
env {
  name = atlas.env
  dev  = "sqlite://file?mode=memory"
  url  = "sqlite://file.db"
  migration {
    dir = "file://migrations"
  }
}
`, string(dst.Bytes()))

	// Merge with existing env block and un-named env block.
	dst, err = parseConfig(`
env "foo" {
}
env {
	name = atlas.env
}
`)
	require.NoError(t, err)
	require.NoError(t, mergeEnvBlock(dst.Body(), envBlock, "foo"))
	require.Equal(t, `
env "foo" {
  dev = "sqlite://file?mode=memory"
  url = "sqlite://file.db"
  migration {
    dir = "file://migrations"
  }
}
env {
  name = atlas.env
}
`, string(dst.Bytes()))

	// Merge with un-named env block.
	dst, err = parseConfig(`
env "foo" {
}
env {
	name = atlas.env
}
`)
	require.NoError(t, err)
	require.NoError(t, mergeEnvBlock(dst.Body(), envBlock, "bar"))
	require.Equal(t, `
env "foo" {
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

func parseConfig(cfg string) (*hclwrite.File, error) {
	f, diags := hclwrite.ParseConfig([]byte(cfg), "atlas.hcl", hcl.InitialPos)
	if diags.HasErrors() {
		return nil, diags
	}
	return f, nil
}
