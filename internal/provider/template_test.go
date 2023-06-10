package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTemplate(t *testing.T) {
	var update = true
	tests := []struct {
		name string
		data templateData
	}{
		{name: "token", data: templateData{
			URL: "mysql://user:pass@localhost:3306/tf-db",
			Cloud: &cloudConfig{
				Token: "token",
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
