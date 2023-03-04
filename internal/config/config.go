package config

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	hcl "github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

type Config struct {
	ProviderMirrors map[string]*ProviderMirror

	Filename string
}

type ProviderMirror struct {
	Name       string
	OriginURL  *url.URL
	NamePrefix string

	DeclRange hcl.Range
}

func LoadConfigFile(filename string) (*Config, hcl.Diagnostics) {
	src, err := os.ReadFile(filename)
	if err != nil {
		return nil, hcl.Diagnostics{
			{
				Severity: hcl.DiagError,
				Summary:  "Cannot read configuration file",
				Detail:   fmt.Sprintf("Failed to read %s: %s.", filename, err),
			},
		}
	}
	return LoadConfig(src, filename)
}

func LoadConfig(src []byte, filename string) (*Config, hcl.Diagnostics) {
	f, diags := hclsyntax.ParseConfig(src, filename, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, diags
	}

	content, moreDiags := f.Body.Content(rootSchema)
	diags = append(diags, moreDiags...)
	if moreDiags.HasErrors() {
		return nil, diags
	}

	ret := &Config{
		Filename:        filename,
		ProviderMirrors: make(map[string]*ProviderMirror),
	}
	namesUsed := make(map[string]hcl.Range)

	for _, block := range content.Blocks {

		switch block.Type {
		case "provider_mirror":
			mirror, moreDiags := decodeProviderMirror(block)
			if existingRng, exists := namesUsed[mirror.Name]; exists {
				moreDiags = moreDiags.Append(&hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Duplicate Service Name",
					Detail:   fmt.Sprintf("A service named %q was already declared at %s. Service names must be unique.", mirror.Name, existingRng),
					Subject:  &block.DefRange,
				})
			}
			diags = append(diags, moreDiags...)
			namesUsed[mirror.Name] = mirror.DeclRange
			if moreDiags.HasErrors() {
				continue
			}

			ret.ProviderMirrors[mirror.Name] = mirror

		default:
			// Should not get here because only the cases above are in our schema.
			panic(fmt.Sprintf("unexpected block type %q", block.Type))
		}
	}

	return ret, diags
}

func decodeProviderMirror(block *hcl.Block) (*ProviderMirror, hcl.Diagnostics) {
	ret := &ProviderMirror{
		Name:      block.Labels[0],
		DeclRange: block.DefRange,
	}

	type Config struct {
		OriginURL  gohcl.WithRange[string] `hcl:"origin_url"`
		NamePrefix gohcl.WithRange[string] `hcl:"name_prefix"`
	}
	var config Config
	diags := gohcl.DecodeBody(block.Body, nil, &config)
	if diags.HasErrors() {
		return ret, diags
	}

	var err error
	ret.OriginURL, err = url.Parse(config.OriginURL.Value)
	if err != nil {
		diags = diags.Append(&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Invalid OCI repository origin URL",
			Detail:   fmt.Sprintf("Invalid URL syntax: %s.", err),
			Subject:  config.OriginURL.Range.Ptr(),
		})
	} else {
		if ret.OriginURL.Scheme != "http" && ret.OriginURL.Scheme != "https" {
			diags = diags.Append(&hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Invalid OCI repository origin URL",
				Detail:   "OCI registry URL must use either the 'https' or 'http' scheme.",
				Subject:  config.OriginURL.Range.Ptr(),
			})
		} else if !strings.HasSuffix(ret.OriginURL.Path, "/") {
			diags = diags.Append(&hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Invalid OCI repository origin URL",
				Detail:   "OCI registry URL have a path ending with a slash '/'.",
				Subject:  config.OriginURL.Range.Ptr(),
			})
		}
	}

	ret.NamePrefix = config.NamePrefix.Value
	if !ociNamePattern.MatchString(ret.NamePrefix) {
		diags = diags.Append(&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Invalid OCI repository name prefix",
			Detail:   "Must be one or more registry name segments separated by slashes.",
			Subject:  config.NamePrefix.Range.Ptr(),
		})
	}

	return ret, diags
}

var rootSchema = &hcl.BodySchema{
	Blocks: []hcl.BlockHeaderSchema{
		{Type: "provider_mirror", LabelNames: []string{"name"}},
	},
}

var ociNamePattern = regexp.MustCompile(`[a-z0-9]+([._-][a-z0-9]+)*(/[a-z0-9]+([._-][a-z0-9]+)*)*`)
