package config

import (
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/apparentlymart/oci-distribution-terraform-registry/internal/ocidist"
	hcl "github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

type Config struct {
	ProviderMirrors map[string]*ProviderMirror
	Server          *Server

	Filename string
}

type ProviderMirror struct {
	Name          string
	OriginURL     *url.URL
	NamePrefix    ocidist.Namespace
	ProxyPackages bool

	DeclRange hcl.Range
}

type Server struct {
	ListenAddr string
	TLS        *TLSConfig

	QueryStringSecret *[32]byte

	DeclRange hcl.Range
}

type TLSConfig struct {
	Certificate tls.Certificate
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
					Summary:  "Duplicate service name",
					Detail:   fmt.Sprintf("A service named %q was already declared at %s. Service names must be unique.", mirror.Name, existingRng),
					Subject:  block.DefRange.Ptr(),
				})
			}
			diags = append(diags, moreDiags...)
			namesUsed[mirror.Name] = mirror.DeclRange
			if moreDiags.HasErrors() {
				continue
			}

			ret.ProviderMirrors[mirror.Name] = mirror

		case "server":
			serverConfig, moreDiags := decodeServerConfig(block)
			diags = append(diags, moreDiags...)
			if ret.Server != nil {
				diags = diags.Append(&hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Duplicate server configuration",
					Detail:   fmt.Sprintf("The server was already configured at %s.", ret.Server.DeclRange),
					Subject:  block.DefRange.Ptr(),
				})
				continue
			}
			ret.Server = serverConfig

		default:
			// Should not get here because only the cases above are in our schema.
			panic(fmt.Sprintf("unexpected block type %q", block.Type))
		}
	}

	diags = append(diags, validate(ret)...)

	return ret, diags
}

func validate(cfg *Config) hcl.Diagnostics {
	var diags hcl.Diagnostics

	for _, mirror := range cfg.ProviderMirrors {
		if mirror.ProxyPackages && cfg.Server.QueryStringSecret == nil {
			diags = diags.Append(&hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Query string secret required for package proxy",
				Detail:   "The proxy_packages option requires that you set the query_string_secret argument inside the server block, to provide a secret key used to authenticate package download requests.",
				Subject:  mirror.DeclRange.Ptr(),
			})
		}
	}

	return diags
}

func decodeProviderMirror(block *hcl.Block) (*ProviderMirror, hcl.Diagnostics) {
	ret := &ProviderMirror{
		Name:      block.Labels[0],
		DeclRange: block.DefRange,
	}

	type Config struct {
		OriginURL     gohcl.WithRange[string] `hcl:"origin_url"`
		NamePrefix    gohcl.WithRange[string] `hcl:"name_prefix"`
		ProxyPackages bool                    `hcl:"proxy_packages"`
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

	namePrefix, err := ocidist.ParseNamespace(config.NamePrefix.Value)
	if err != nil {
		diags = diags.Append(&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Invalid OCI repository name prefix",
			Detail:   fmt.Sprintf("Incorrect OCI distribution namespace syntax: %s.", err),
			Subject:  config.NamePrefix.Range.Ptr(),
		})
	} else {
		ret.NamePrefix = namePrefix
	}

	ret.ProxyPackages = config.ProxyPackages

	return ret, diags
}

func decodeServerConfig(block *hcl.Block) (*Server, hcl.Diagnostics) {
	ret := &Server{
		DeclRange: block.DefRange,
	}

	type TLSConfigHCL struct {
		CertificateFile gohcl.WithRange[string] `hcl:"certificate_file"`
		PrivateKeyFile  gohcl.WithRange[string] `hcl:"private_key_file"`
	}
	type Config struct {
		ListenAddr        gohcl.WithRange[*string] `hcl:"listen_addr,optional"`
		TLS               *TLSConfigHCL            `hcl:"tls,block"`
		QueryStringSecret gohcl.WithRange[*string] `hcl:"query_string_secret,optional"`
	}
	var config Config
	diags := gohcl.DecodeBody(block.Body, nil, &config)
	if diags.HasErrors() {
		return ret, diags
	}

	if config.ListenAddr.Value != nil {
		_, _, err := net.SplitHostPort(*config.ListenAddr.Value)
		if err != nil {
			diags = diags.Append(&hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Invalid listen address",
				Detail:   "Listen address must be an IP address followed by a colon and then a port number.",
				Subject:  config.ListenAddr.Range.Ptr(),
			})
		} else {
			ret.ListenAddr = *config.ListenAddr.Value
		}
	}

	if config.QueryStringSecret.Value != nil {
		inHex := *config.QueryStringSecret.Value
		if len(inHex) != 64 {
			diags = diags.Append(&hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Invalid query string secret",
				Detail:   "Must be exactly 64 hexadecimal digits, representing a 256-bit secret key.",
				Subject:  config.QueryStringSecret.Range.Ptr(),
			})
		} else if raw, err := hex.DecodeString(inHex); err != nil {
			diags = diags.Append(&hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Invalid query string secret",
				Detail:   fmt.Sprintf("Must be exactly 64 hexadecimal digits, representing a 256-bit secret key: %s.", err),
				Subject:  config.QueryStringSecret.Range.Ptr(),
			})
		} else {
			var key [32]byte
			copy(key[:], raw)
			ret.QueryStringSecret = &key
		}
	}

	if config.TLS != nil {
		var tlsDiags hcl.Diagnostics

		certFilename := config.TLS.CertificateFile.Value
		keyFilename := config.TLS.PrivateKeyFile.Value
		basePath := filepath.Dir(block.DefRange.Filename)
		if !filepath.IsAbs(certFilename) {
			certFilename = filepath.Join(basePath, certFilename)
		}
		if !filepath.IsAbs(keyFilename) {
			keyFilename = filepath.Join(basePath, keyFilename)
		}

		diags = append(diags, tlsDiags...)
		if !tlsDiags.HasErrors() {
			tlsDiags = tlsDiags[:0]
			cert, err := tls.LoadX509KeyPair(certFilename, keyFilename)
			if err != nil {
				tlsDiags = tlsDiags.Append(&hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Failed to parse TLS keypair",
					Detail:   fmt.Sprintf("Cannot build a valid TLS configuration from the specified certificate and private key: %s.", err),
					Subject:  config.TLS.CertificateFile.Range.Ptr(),
				})
			}
			diags = append(diags, tlsDiags...)
			if !tlsDiags.HasErrors() {
				ret.TLS = &TLSConfig{
					Certificate: cert,
				}
			}
		}
	}

	return ret, diags
}

var rootSchema = &hcl.BodySchema{
	Blocks: []hcl.BlockHeaderSchema{
		{Type: "provider_mirror", LabelNames: []string{"name"}},
		{Type: "server"},
	},
}
