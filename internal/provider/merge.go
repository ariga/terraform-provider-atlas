package provider

import (
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclwrite"
)

func parseConfig(cfg string) (*hclwrite.File, error) {
	f, diags := hclwrite.ParseConfig([]byte(cfg), "atlas.hcl", hcl.InitialPos)
	if diags.HasErrors() {
		return nil, diags
	}
	return f, nil
}

func mergeFile(dst, src *hclwrite.File) {
	dstBody, srcBody := dst.Body(), src.Body()
	dstBlocks := make(map[string]*hclwrite.Block)
	for _, blk := range dstBody.Blocks() {
		dstBlocks[address(blk)] = blk
	}
	for _, blk := range srcBody.Blocks() {
		if dstBlk, ok := dstBlocks[address(blk)]; ok {
			// Merge the blocks if they have the same address.
			mergeBlock(dstBlk, blk)
		} else {
			dstBody.AppendBlock(blk)
		}
	}
}

func address(block *hclwrite.Block) string {
	parts := append([]string{block.Type()}, block.Labels()...)
	return strings.Join(parts, ".")
}

func mergeBlock(dst, src *hclwrite.Block) {
	dstBody, srcBody := dst.Body(), src.Body()
	for name, attr := range srcBody.Attributes() {
		dstBody.SetAttributeRaw(name, attr.Expr().BuildTokens(nil))
	}
	srcBlocks := srcBody.Blocks()
	srcBlockTypes := make(map[string]struct{})
	for _, blk := range srcBlocks {
		srcBlockTypes[blk.Type()] = struct{}{}
	}
	for _, blk := range dstBody.Blocks() {
		if _, conflict := srcBlockTypes[blk.Type()]; conflict {
			// Remove the block from the destination if it already exists.
			dstBody.RemoveBlock(blk)
		}
	}
	for _, blk := range srcBlocks {
		dstBody.AppendBlock(blk)
	}
}
