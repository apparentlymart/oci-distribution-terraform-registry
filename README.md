# Terraform Registry and Mirror Proxy for OCI Distribution Registries

This repository contains a (currently experimental) proxy server which aims to
provide Terraform Registry and Mirror protocols as a server by translating
requests to OCI Distribution registries as a client.

This is not a HashiCorp project. It's also incomplete, so it may not have all
features needed to successfully run a server for production use.

Currently the server supports only
[Terraform's Provider Mirror protocol](https://developer.hashicorp.com/terraform/internals/provider-network-mirror-protocol),
which allows using objects in an OCI registry as a second source for Terraform
providers that would normally be hosted in some other registry.

## Usage

This is a Go program. To build it, clone the repository and use the Go toolchain
(Go 1.19 or later) to build the executable from the root of the repository:

```
go build -o ~/bin/oci-distribution-terraform-registry .
```

You should then be able to run that program:

```
~/bin/oci-distribution-terraform-registry
```

It will complain that it needs a configuration file and describe all of the
locations where it will automatically look to find one. Alternatively you can
use the `--config=FILENAME` option to specify a configuration file location
directly on the command line.

## Configuration Format

The configuration file format is based on HCL grammar, meaning that its syntax
is similar to that used in the Terraform language itself.

See `example-config.hcl` for an example configuration wrapping an OCI
Distribution registry running on localhost.

## Running the Server

The main thing to do with this program is to run its server. Use the `server`
subcommand to start it, once you have a valid configuration:

```
~/bin/oci-distribution-terraform-registry server
```

## Authentication

The server delegates authentication entirely to the underlying OCI Distribution
registries. Terraform itself only supports bearer-token-based authentication,
and so you will need to select an OCI Distribution registry implementation that
itself supports bearer tokens.

Configure a bearer token for your server using
[a `credentials` block in your CLI configuration](https://developer.hashicorp.com/terraform/cli/config/config-file#credentials-1),
or equivalently a suitably-named environment variable or a credentials helper
program.

## Provider Mirror Services

Use a `provider_mirror` block in your configuration to declare a service
implementing [Terraform's Provider Mirror Protocol](https://developer.hashicorp.com/terraform/internals/provider-network-mirror-protocol).

```hcl
provider_mirror "mirror" {
  # The URL of the OCI Distribution registry containing the packages.
  origin_url = "http://127.0.0.1:5000/"

  # The namespace prefix where the provider package manifests will be
  # registered. The provider's own address will be appended to this, so
  # with the example value below the full namespace might be something
  # like: terraform-providers/registry.terraform.io/hashicorp/aws .
  name_prefix = "terraform-providers"

  // If the underlying OCI Distribution registry requires a bearer token when
  // downloading then you must enable this setting so that the provider mirror
  // will insert the auth credentials when handling download requests.
  proxy_packages = true
}
```

The service declared in the above example is named "mirror", so its base URL
will be your server's URL with that service path appended. For example, if
your server is deployed at `https://example.com/` then the mirror URL will
be `https://example.com/mirror/`.

Terraform requires that network mirrors run at `https:` URLs, so you will need
to include TLS configuration in your server settings or alternatively place
the server behind a load balancer or other proxy that is able to terminate
TLS on the server's behalf.

## Provider Registry Services

This program doesn't yet support Terraform's provider registry protocol, so
you can't use the server as an origin registry for providers you've developed
yourself.

However, you could instead place your own providers into a provider mirror
service alongside upstream providers, by placing them under a hostname that
you control.

## Module Registry Services

This program doesn't yet support Terraform's module registry protocol, so
you can't use the server as an origin registry for distributing modules.

## Contributing

If you're interested in adding something to this project, please open an issue
first to discuss what you'd like to implement.

If you open a pull request then I'll assume you intend to offer the contributed
code under the terms of the MIT license and that you have the rights to do so.
