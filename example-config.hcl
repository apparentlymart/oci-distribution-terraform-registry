# This is an example configuration to assist in early experimentation with
# this program. This exact configuration is not suitable for production use,
# since it doesn't use TLS, listens only on localhost and assumes you have an
# OCI Distribution registry running on localhost port 5000.

provider_mirror "mirror" {
  origin_url  = "http://127.0.0.1:5000/"
  name_prefix = "terraform-providers"
}

server {
  listen_addr = "localhost:8080"
}
