package provider

import (
	"context"
	"fmt"

	// The following imports are required to register the SQL drivers.
	// For emptySchema() function to work.
	_ "ariga.io/atlas/sql/mysql"
	_ "ariga.io/atlas/sql/postgres"
	_ "ariga.io/atlas/sql/sqlite"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"

	"ariga.io/atlas/sql/sqlclient"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// emptySchema returns an empty schema block if the URL is connected to a schema.
// Otherwise, it returns a null schema block.
func emptySchema(ctx context.Context, url string, hcl *types.String) (diags diag.Diagnostics) {
	s, err := sqlclient.Open(ctx, url)
	if err != nil {
		diags.AddError("Atlas Plan Error",
			fmt.Sprintf("Unable to connect to database, got error: %s", err),
		)
		return
	}
	defer s.Close()
	name := s.URL.Schema
	if name != "" {
		*hcl = types.StringValue(fmt.Sprintf("schema %q {}", name))
		return
	}
	*hcl = types.StringNull()
	return diags
}
