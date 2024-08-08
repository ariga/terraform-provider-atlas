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
	schemas := 0
	for _, blk := range root.Blocks() {
		if blk.Type() == "schema" {
			schemas++
			// Clear the schema block to get an empty schema.
			blk.Body().Clear()
		} else {
			// Remove all other blocks.
			root.RemoveBlock(blk)
		}
	}
	if schemas > 1 {
		// If there are more than one schema blocks, return an empty string.
		// To clear the whole realm
		return "", nil
	}
	return string(f.Bytes()), nil
}
