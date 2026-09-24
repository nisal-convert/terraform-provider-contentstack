# Contentstack Terraform Provider

The Terraform provider for [Contentstack](https://www.contentstack.com/) allows
you to configure your Contentstack stack with infrastructure-as-code principles.

## Fork status

This fork adds content-type `options` and `maintain_revisions`, and `field_rules`
for content types and global fields. It preserves JSON options that the upstream
SDK does not expose, including boolean URL settings and unknown properties.

The fork is not yet published to the Terraform Registry. Its provider address is
`nisal-convert/contentstack`; the upstream `labd/contentstack` release does not
contain these changes.

For local testing, build the provider and configure a Terraform
[development override](https://developer.hashicorp.com/terraform/cli/config/config-file#development-overrides-for-provider-developers)
for `nisal-convert/contentstack` pointing to the absolute path of `local/provider`:

```sh
go build -o local/provider/terraform-provider-contentstack .
CONTENTSTACK_TEST_TERRAFORM="$(command -v terraform)" \
  CONTENTSTACK_TEST_PROVIDER_DIR="$PWD/local/provider" \
  go test -race ./...
```

The integration tests use a local mock API and dummy credentials. They do not
contact a Contentstack stack. Normalize JSON configuration with `jsonencode` to
avoid formatting-only differences after import.

## Upstream usage

The full documentation is available via https://registry.terraform.io/providers/labd/contentstack/latest/docs

Add the following to your terraform project:

```hcl
terraform {
  required_providers {
    contentstack = {
      source = "labd/contentstack"
    }
  }
}
```

## Authors

This project is developed by [Lab Digital](https://www.labdigital.nl). We
welcome additional contributors. Please see our
[GitHub repository](https://github.com/labd/terraform-provider-contentstack)
for more information.
