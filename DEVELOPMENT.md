# Development Guide for terraform-provider-atlas

This document provides guidelines for developing and testing the terraform-provider-atlas provider.

## Testing the Provider Locally

Testing is essential for maintaining provider quality and functionality. Follow these approaches to test your provider implementation during development.

### Method 1: Install Provider Locally and Test with Terraform

Follow these steps to build and test the provider locally:

1. Install the provider binary:

```shell
go install .
```

This will compile and install the binary in your `$GOPATH/bin` directory.

2. Create or modify the `.terraformrc` file in your home directory:

```hcl
provider_installation {
    dev_overrides {
        "ariga/atlas" = "$GOPATH/bin"
    }

    # This enables standard providers to work alongside your local version
    direct {}
}
```

3. Create a test directory with a `main.tf` file:

```hcl
terraform {
    required_providers {
        atlas = {
        source = "ariga/atlas"
        }
    }
}

data "atlas_schema" "db" {
    # Your configuration here
}

resource "atlas_schema" "db" {
    # Your configuration here
}
 ```

4. Initialize and apply your Terraform configuration:

```shell
terraform init
terraform apply
```

### Method 2: Run in Debug Mode

For interactive debugging during development:

#### Option A: Command Line

Run the provider in debug mode using:

```shell
go run ./main.go --debug
```

#### Option B: VS Code

Create a `.vscode/launch.json` file with:

```json
{
  "version": "0.2.0",
  "configurations": [
     {
        "name": "Launch Package",
        "type": "go",
        "request": "launch",
        "mode": "auto",
        "program": "${workspaceFolder}",
        "args": ["--debug"],
        "env": {}
     }
  ]
}
```

#### Using the Debug Provider

When running in debug mode, the provider will output a connection string like:

```
Provider started. To attach Terraform CLI, set the TF_REATTACH_PROVIDERS environment variable with the following:

TF_REATTACH_PROVIDERS='{"registry.terraform.io/ariga/atlas":{"Protocol":"grpc","ProtocolVersion":6,"Pid":75628,"Test":true,"Addr":{"Network":"unix","String":"/var/folders/wd/1xy9p8053b137plg1yxc6c_00000gn/T/plugin1495999028"}}}'
```

Set this environment variable in your terminal session, then run Terraform commands to test the provider:

```shell
export TF_REATTACH_PROVIDERS='...'
terraform init
terraform apply
```