default: testacc

# Run acceptance tests
.PHONY: testacc
testacc:
	TF_ACC=1 go test ./... -v $(TESTARGS) -timeout 120m

# Upgrade terraform plugin framework to latest version
.PHONY: upgrade-tf
upgrade-tf:
	go get github.com/hashicorp/terraform-plugin-framework@latest
	go mod tidy