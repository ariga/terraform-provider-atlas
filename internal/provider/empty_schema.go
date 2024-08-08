package provider

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclwrite"
)

// emptySchemas removes all all blocks except the schema block from the given schema.
func emptySchemas(schema string) (string, error) {
	f, diags := hclwrite.ParseConfig([]byte(schema), "schema.hcl", hcl.InitialPos)
	if diags.HasErrors() {
		return "", diags
	}
	root := f.Body()
	for _, blk := range root.Blocks() {
		if blk.Type() == "schema" {
			// Clear the schema block to get an empty schema.
			blk.Body().Clear()
		} else {
			// Remove all other blocks.
			root.RemoveBlock(blk)
		}
	}
	return string(f.Bytes()), nil
}
