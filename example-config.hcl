# This is an example configuration to assist in early experimentation with
# this program. This exact configuration is not suitable for production use,
# since it doesn't use TLS, listens only on localhost and assumes you have an
# OCI Distribution registry running on localhost port 5000.

# provider_mirror defines a Terraform Provider Mirror Protocol service.
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

server {
  listen_addr = "localhost:8080"

  # This secret is used whenever the proxy needs to pass some secret data
  # through the query string in order to properly implement one of Terraform's
  # protocols. In a real configuration you should use 32 bytes generated by a
  # random number generator suitable for cryptography, and keep this secret
  # safe from anyone who wouldn't otherwise be able to see authentication
  # tokens passing through the server in "Authorization" headers.
  query_string_secret = "0000000000000000000000000000000000000000000000000000000000000000"

  # For a real server you'll need to use TLS, because Terraform requires that
  # for some of its protocols.
  #tls {
  #  certificate_file = "certs.pem"
  #  private_key_file = "private_key.pem"
  #}
}
