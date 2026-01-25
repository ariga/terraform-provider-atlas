package provider

import (
	"bytes"
	"context"
	"net/url"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/require"
)

func TestAtlasSchemaDataSource_WorkspaceConfig(t *testing.T) {
	tests := []struct {
		name        string
		src         string
		devURL      string
		expected    string
		expectedURL string // Expected RepoURL result
	}{
		{
			name:   "atlas:// cloud URL",
			src:    "atlas://my-repo",
			devURL: "docker://postgres/16/dev?search_path=public",
			expected: `env "tf" {
  dev = "docker://postgres/16/dev?search_path=public"
  url = "atlas://my-repo"
}
`,
			expectedURL: "atlas://my-repo",
		},
		{
			name:   "atlas:// cloud URL with tag",
			src:    "atlas://my-repo?tag=v1.0.0",
			devURL: "docker://mysql/8",
			expected: `env "tf" {
  dev = "docker://mysql/8"
  url = "atlas://my-repo?tag=v1.0.0"
}
`,
			expectedURL: "atlas://my-repo", // Tag should be stripped from RepoURL
		},
		{
			name:   "inline HCL",
			src:    `schema "test" {}`,
			devURL: "docker://postgres/16/dev",
			expected: `env "tf" {
  dev = "docker://postgres/16/dev"
  url = "file://schema.hcl"
}
`,
			expectedURL: "", // No cloud URL for inline HCL
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &AtlasSchemaDataSourceModel{
				Src:    types.StringValue(tt.src),
				DevURL: types.StringValue(tt.devURL),
			}
			providerData := &ProviderData{}

			// Create a mock client factory that doesn't actually connect
			providerData.Client = func(wd string, c *CloudConfig) (AtlasExec, error) {
				return nil, nil
			}

			w, cleanup, err := data.Workspace(context.Background(), providerData)
			require.NoError(t, err)
			defer cleanup()

			// Verify the generated config has the correct URL
			buf := &bytes.Buffer{}
			require.NoError(t, w.Project.Render(buf))
			require.Equal(t, tt.expected, buf.String())

			// Verify RepoURL extracts the correct repo from the URL
			repoURL, err := w.Project.RepoURL()
			require.NoError(t, err)
			if tt.expectedURL == "" {
				require.Nil(t, repoURL)
			} else {
				require.NotNil(t, repoURL)
				require.Equal(t, tt.expectedURL, repoURL.String())
			}
		})
	}
}

func TestAtlasSchemaDataSource_RepoURLNotOverriddenByProviderConfig(t *testing.T) {
	// This test verifies that when using atlas:// URL in src,
	// the repo name comes from the URL, NOT from the provider's cloud.repo config
	data := &AtlasSchemaDataSourceModel{
		Src:    types.StringValue("atlas://open-taco"),
		DevURL: types.StringValue("docker://postgres/16/dev"),
	}
	providerData := &ProviderData{
		// Simulate provider having a different repo configured
		Cloud: &AtlasCloudBlock{
			Repo:  types.StringValue("different-repo"),
			Token: types.StringValue("token"),
		},
	}
	providerData.Client = func(wd string, c *CloudConfig) (AtlasExec, error) {
		return nil, nil
	}

	w, cleanup, err := data.Workspace(context.Background(), providerData)
	require.NoError(t, err)
	defer cleanup()

	// Verify RepoURL returns the URL from src, not from provider config
	repoURL, err := w.Project.RepoURL()
	require.NoError(t, err)
	require.NotNil(t, repoURL)

	// Should be "open-taco" from the atlas:// URL, NOT "different-repo" from provider
	expected, _ := url.Parse("atlas://open-taco")
	require.Equal(t, expected.String(), repoURL.String())
}
