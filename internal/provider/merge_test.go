package provider

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_mergeFile(t *testing.T) {
	dst, err := parseConfig(`
atlas {
}
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
  migration {
    dir = "file://migrations"
  }
}
`, string(dst.Bytes()))
}
