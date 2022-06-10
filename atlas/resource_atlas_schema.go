package atlas

import (
	atlaschema "ariga.io/atlas/sql/schema"
	"ariga.io/atlas/sql/sqlclient"
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func newSchemaResource() *schema.Resource {
	return &schema.Resource{
		Description: "Atlas database resource manages the data schema of the database, using an HCL file describing the wanted state of the database. see https://atlasgo.io/",
		// Create&Update both apply migrations
		CreateContext: applySchema,
		UpdateContext: applySchema,
		ReadContext:   readSchema,
		DeleteContext: deleteSchema,
		CustomizeDiff: customizeDiff,
		Schema: map[string]*schema.Schema{
			"hcl": {
				Type:        schema.TypeString,
				Description: "The schema definition for the database (preferably normalized - see `atlas_schema` data source)",
				Required:    true,
			},
			"url": {
				Type:        schema.TypeString,
				Description: "The url of the database see https://atlasgo.io/cli/url",
				Required:    true,
				Sensitive:   true,
			},
			"dev_db_url": {
				Description: "The url of the dev-db see https://atlasgo.io/cli/url",
				Type:        schema.TypeString,
				Optional:    true,
				Sensitive:   true,
			},
		},
	}
}

func customizeDiff(ctx context.Context, diff *schema.ResourceDiff, i interface{}) error {
	oldV, newV := diff.GetChange("hcl")
	if oldV == nil {
		return nil
	}
	s, ok := oldV.(string)
	if !ok || s != "" {
		return nil
	}
	url := diff.Get("url").(string)
	cli, err := sqlclient.Open(ctx, url)
	if err != nil {
		return err
	}
	var schemas []string
	if cli.URL.Schema != "" {
		schemas = append(schemas, cli.URL.Schema)
	}
	current, err := cli.InspectRealm(ctx, &atlaschema.InspectRealmOption{Schemas: schemas})
	if err != nil {
		return err
	}
	desired := &atlaschema.Realm{}
	if err = cli.Evaluator.Eval([]byte(newV.(string)), desired, nil); err != nil {
		return err
	}
	changes, err := cli.RealmDiff(current, desired)
	if err != nil {
		return err
	}
	var causes []string
	for _, c := range changes {
		switch c := c.(type) {
		case *atlaschema.DropSchema:
			causes = append(causes, fmt.Sprintf("DROP SCHEMA %q", c.S.Name))
		case *atlaschema.DropTable:
			causes = append(causes, fmt.Sprintf("DROP TABLE %q", c.T.Name))
		case *atlaschema.ModifyTable:
			for _, c1 := range c.Changes {
				if d, ok := c1.(*atlaschema.DropColumn); ok {
					causes = append(causes, fmt.Sprintf("DROP COLUMN %q.%q", c.T.Name, d.C.Name))
				}
			}
		}
	}
	if len(causes) > 0 {
		return fmt.Errorf(`The database contains resources that Atlas wants to drop because they are not defined in the HCL file on the first run.
- %s

To learn how to add an existing database to a project, read: https://atlasgo.io/terraform-provider#working-with-an-existing-database`, strings.Join(causes, "\n- "))
	}
	return nil
}

func deleteSchema(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	url := d.Get("url").(string)

	cli, err := sqlclient.Open(ctx, url)
	if err != nil {
		return diag.FromErr(err)
	}
	var schemas []string
	if cli.URL.Schema != "" {
		schemas = append(schemas, cli.URL.Schema)
	}
	realm, err := cli.InspectRealm(ctx, &atlaschema.InspectRealmOption{Schemas: schemas})
	if err != nil {
		return diag.FromErr(err)
	}
	desired := &atlaschema.Realm{}
	changes, err := cli.RealmDiff(realm, desired)
	if err != nil {
		return diag.FromErr(err)
	}
	if err = cli.ApplyChanges(ctx, changes); err != nil {
		return diag.FromErr(err)
	}
	return diags
}

func readSchema(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	url := d.Get("url").(string)

	cli, err := sqlclient.Open(ctx, url)
	if err != nil {
		return diag.FromErr(err)
	}
	var schemas []string
	if cli.URL.Schema != "" {
		schemas = append(schemas, cli.URL.Schema)
	}
	realm, err := cli.InspectRealm(ctx, &atlaschema.InspectRealmOption{Schemas: schemas})
	if err != nil {
		return diag.FromErr(err)
	}
	hcl, err := cli.MarshalSpec(realm)
	if err != nil {
		return diag.FromErr(err)
	}

	d.Set("hcl", string(hcl))
	d.Set("url", url)
	return diags
}

func applySchema(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	url := d.Get("url").(string)
	hcl := d.Get("hcl").(string)

	cli, err := sqlclient.Open(ctx, url)
	if err != nil {
		return diag.FromErr(err)
	}
	var schemas []string
	if cli.URL.Schema != "" {
		schemas = append(schemas, cli.URL.Schema)
	}
	realm, err := cli.InspectRealm(ctx, &atlaschema.InspectRealmOption{Schemas: schemas})
	if err != nil {
		return diag.FromErr(err)
	}
	desired := &atlaschema.Realm{}
	if err = cli.Evaluator.Eval([]byte(hcl), desired, nil); err != nil {
		return diag.FromErr(err)
	}
	if devURL, ok := d.GetOk("dev_db_url"); ok {
		dev, err := sqlclient.Open(ctx, devURL.(string))
		if err != nil {
			return diag.FromErr(err)
		}
		defer dev.Close()
		desired, err = dev.Driver.(atlaschema.Normalizer).NormalizeRealm(ctx, desired)
		if err != nil {
			return diag.FromErr(err)
		}
	} else {
		diags = append(diags, diag.Diagnostic{
			Severity:      diag.Warning,
			AttributePath: cty.GetAttrPath("dev_db_url"),
			Summary:       "it is highly recommended that you use 'dev_db_url' to specify a dev database.\nto learn more about it, visit: https://atlasgo.io/dev-database",
		})
	}

	changes, err := cli.RealmDiff(realm, desired)
	if err != nil {
		return diag.FromErr(err)
	}
	if err = cli.ApplyChanges(ctx, changes); err != nil {
		return diag.FromErr(err)
	}
	d.SetId(url)

	desiredHCL, err := cli.MarshalSpec(desired)
	if err != nil {
		return diag.FromErr(err)
	}
	d.Set("hcl", string(desiredHCL))
	return diags
}
