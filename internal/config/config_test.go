package config

import (
	"crypto/tls"
	"net/url"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/hcl/v2"
)

func TestLoadConfig(t *testing.T) {
	src := []byte(`
		provider_mirror "mirror" {
			origin_url  = "http://127.0.0.1:5000/"
			name_prefix = "terraform-providers"
		}

		server {
			listen_addr = ":8080"
			tls {
				certificate_file = "certs.pem"
				private_key_file = "private_key.pem"
			}
		}
	`)

	cert, err := tls.LoadX509KeyPair("testdata/certs.pem", "testdata/private_key.pem")
	if err != nil {
		t.Fatalf("failed to load TLS certificate to test with: %s", err)
	}

	gotConfig, diags := LoadConfig(src, "testdata/test.hcl")
	if diags.HasErrors() {
		t.Fatalf("unexpected errors: %s", diags.Error())
	}
	wantConfig := &Config{
		Filename: "testdata/test.hcl",
		ProviderMirrors: map[string]*ProviderMirror{
			"mirror": {
				Name: "mirror",
				OriginURL: &url.URL{
					Scheme: "http",
					Host:   "127.0.0.1:5000",
					Path:   "/",
				},
				NamePrefix: "terraform-providers",
				DeclRange: hcl.Range{
					Filename: "testdata/test.hcl",
					Start:    hcl.Pos{Line: 2, Column: 3, Byte: 3},
					End:      hcl.Pos{Line: 2, Column: 27, Byte: 27},
				},
			},
		},
		Server: &Server{
			ListenAddr: ":8080",
			TLS: &TLSConfig{
				Certificate: cert,
			},
			DeclRange: hcl.Range{
				Filename: "testdata/test.hcl",
				Start:    hcl.Pos{Line: 7, Column: 3, Byte: 118},
				End:      hcl.Pos{Line: 7, Column: 9, Byte: 124},
			},
		},
	}

	if diff := cmp.Diff(wantConfig, gotConfig); diff != "" {
		t.Errorf("wrong config\n%s", diff)
	}
}
