package config

import (
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
	`)

	gotConfig, diags := LoadConfig(src, "test.hcl")
	if diags.HasErrors() {
		t.Fatalf("unexpected errors: %s", diags.Error())
	}
	wantConfig := &Config{
		Filename: "test.hcl",
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
					Filename: "test.hcl",
					Start:    hcl.Pos{Line: 2, Column: 3, Byte: 3},
					End:      hcl.Pos{Line: 2, Column: 27, Byte: 27},
				},
			},
		},
	}

	if diff := cmp.Diff(wantConfig, gotConfig); diff != "" {
		t.Errorf("wrong config\n%s", diff)
	}
}
