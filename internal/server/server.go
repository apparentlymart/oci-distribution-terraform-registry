package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/apparentlymart/go-versions/versions"
	"github.com/apparentlymart/oci-distribution-terraform-registry/internal/config"
	"github.com/apparentlymart/oci-distribution-terraform-registry/internal/logging"
	"github.com/apparentlymart/oci-distribution-terraform-registry/internal/ocidist"
	"github.com/apparentlymart/oci-distribution-terraform-registry/internal/querysecret"
)

func Run(ctx context.Context, config *config.Config) error {
	// Query string secret is optional, but the config package should validate
	// that it always be set if any service will rely on it. Code below will
	// assume that secreter is always non-nil if any features that use it are
	// enabled.
	var secreter *querysecret.Secreter
	if config.Server.QueryStringSecret != nil {
		secreter = querysecret.NewSecreter(*config.Server.QueryStringSecret)
	}

	mux := http.NewServeMux()

	for _, mirrorSvc := range config.ProviderMirrors {
		mux.HandleFunc(providerMirrorHandler(mirrorSvc, secreter))
	}

	httpServer := &http.Server{
		Addr:    config.Server.ListenAddr,
		Handler: mux,
		BaseContext: func(l net.Listener) context.Context {
			return ctx
		},
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			ctx = context.WithValue(ctx, remoteAddrContextKey, c.RemoteAddr())
			logPrefix := fmt.Sprintf("[%s] ", c.RemoteAddr())
			logger := log.New(log.Writer(), logPrefix, log.Ldate|log.Ltime|log.LUTC|log.Lmsgprefix)
			return logging.ContextWithLogger(ctx, logger)
		},
	}

	if config.Server.TLS != nil {
		httpServer.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{config.Server.TLS.Certificate},
		}
		log.Printf("HTTPS server listening on %s", config.Server.ListenAddr)
	} else {
		log.Printf("HTTP server listening on %s", config.Server.ListenAddr)
	}

	go func() {
		httpServer.ListenAndServe()
	}()

	<-ctx.Done()
	log.Printf("server shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}

func providerMirrorHandler(cfg *config.ProviderMirror, secreter *querysecret.Secreter) (string, func(resp http.ResponseWriter, req *http.Request)) {
	serviceName := cfg.Name
	prefix := "/" + serviceName + "/"

	ociClient := ocidist.NewClient(cfg.OriginURL)
	userAgent := fmt.Sprintf("oci-distribution-terraform-registry (provider mirror %q)", "serviceName")
	ociClient.AddPrepareRequest(func(req *http.Request) error {
		req.Header.Set("User-Agent", userAgent)

		ctx := req.Context()
		originalReq := contextOriginalReq(ctx)
		if originalReq != nil {
			if a := originalReq.Header.Get("authorization"); a != "" {
				// Pass through the Authorization header to the backend.
				req.Header.Set("Authorization", a)
			}
		}

		return nil
	})

	advertiseHandler := func(resp http.ResponseWriter, req *http.Request) {
		// TODO: A more elaborate page
		content := "<!DOCTYPE html><html><title>Provider Mirror</title><body>This is a Terraform provider mirror.</body></html>"
		resp.Header().Set("Content-Type", "text/html; charset=utf-8")
		resp.Header().Set("Content-Length", strconv.FormatInt(int64(len(content)), 10))
		resp.WriteHeader(200)
		resp.Write([]byte(content))
	}

	return prefix, func(resp http.ResponseWriter, req *http.Request) {
		urlNoQuery := *req.URL
		urlNoQuery.RawQuery = ""
		logger, done := logging.ContextLoggerRequest(req.Context(), "request to provider mirror: %s", &urlNoQuery)
		defer done()

		pathParts := strings.Split(req.URL.EscapedPath(), "/")
		if len(pathParts) == 3 && pathParts[2] == "" {
			// This is a request to our root, which isn't used as part of the
			// mirror protocol and so we'll produce a HTML page advertising
			// the mirror instead.
			advertiseHandler(resp, req)
			return
		}

		if len(pathParts) < 5 {
			// If there aren't at least five parts then there aren't enoough
			// segments to encode a provider address.
			resp.WriteHeader(404)
			return
		}

		addrParts := pathParts[2:5]
		nsAddr, err := ociDistNamespaceFromPathSegments(cfg.NamePrefix, addrParts)
		if err != nil {
			// Can't pass on address that uses characters not allowed by the
			// underlying protocol.
			logger.Printf("unsupported provider address: %s", err)
			resp.WriteHeader(404)
			return
		}
		remainParts := pathParts[5:]
		if len(remainParts) != 1 {
			// Should always have exactly one remaining part, which specifies
			// what about the selected address we are querying.
			resp.WriteHeader(404)
			return
		}
		selector := remainParts[0]

		ctx := contextWithOriginalReq(req.Context(), req)

		if cfg.ProxyPackages {
			if selector == "download" {
				// Our query string should contain an encrypted message that
				// specifies both which object digest we're downloading and
				// possibly an Authorization header value to use when fetching
				// it.
				qs := req.URL.RawQuery
				if len(qs) == 0 {
					logger.Print("missing query string to authenticate the download request")
					resp.WriteHeader(404)
					return
				}
				raw, err := secreter.Unwrap(qs)
				if err != nil {
					logger.Printf("invalid query string argument: %s", err)
					resp.WriteHeader(404)
					return
				}
				firstColon := bytes.IndexByte(raw, ':')
				if firstColon == -1 {
					resp.WriteHeader(404)
					return
				}
				secondColon := firstColon + 1 + bytes.IndexByte(raw[firstColon+1:], ':')
				if secondColon == -1 {
					resp.WriteHeader(404)
					return
				}
				digestStr := string(raw[:secondColon])
				digest, err := ocidist.ParseDigest(digestStr)
				if err != nil {
					logger.Printf("query string has invalid digest %q: %s", digestStr, err)
					resp.WriteHeader(404)
					return
				}

				logger.Printf("proxying content for %s blob %s", nsAddr, digest)
				authHeader := string(raw[secondColon+1:])
				header, r, err := ociClient.GetBlobContent(ctx, nsAddr, digest, authHeader)
				if err != nil {
					propagateOCIDistError(err, resp)
					return
				}
				defer r.Close()

				// We'll copy over all of the server's content-related header
				// fields, such as Content-Type, Content-Length, etc.
				respHeader := resp.Header()
				for n, vs := range header {
					n = textproto.CanonicalMIMEHeaderKey(n)
					if strings.HasPrefix(n, "Content-") {
						respHeader[n] = vs
					}
				}

				resp.WriteHeader(200)
				io.Copy(resp, r)
				return
			}
		}

		if !strings.HasSuffix(selector, ".json") {
			// All selectors always have a .json suffix in the protocol
			resp.WriteHeader(404)
			return
		}
		selector = selector[:len(selector)-5]

		if selector == "index" {
			logger.Printf("fetch tags for %s", nsAddr)
			tags, err := ociClient.GetNamespaceTags(ctx, nsAddr)
			if err != nil {
				propagateOCIDistError(err, resp)
				return
			}
			type RespJSON struct {
				Versions map[string]struct{} `json:"versions"`
			}
			respJSON := RespJSON{make(map[string]struct{})}
			for _, tag := range tags {
				v, err := versions.ParseVersion(tag.String())
				if err != nil {
					continue // Ignore tags that aren't version numbers
				}
				respJSON.Versions[v.String()] = struct{}{}
			}
			respBytes, err := json.Marshal(respJSON)
			if err != nil {
				logger.Printf("failed to serialize JSON response: %s", err)
				resp.WriteHeader(500)
				return
			}
			resp.Header().Set("Content-Length", strconv.FormatInt(int64(len(respBytes)), 10))
			resp.Header().Set("Content-Type", "application/json")
			resp.WriteHeader(200)
			resp.Write(respBytes)
			return
		}

		// If the selector isn't "index" then it should be a version number
		// previously included in a response from index.
		{
			version, err := versions.ParseVersion(selector)
			if err != nil {
				logger.Printf("unsupported selector %q for %s", selector, nsAddr)
				resp.WriteHeader(404)
				return
			}
			tag, err := ocidist.ParseReference(version.String())
			if err != nil {
				logger.Printf("version %s for %s uses version syntax that isn't valid OCI Distribution ref syntax", version, nsAddr)
				resp.WriteHeader(404)
				return
			}
			logger.Printf("fetch layers for %s:%s", nsAddr, tag)
			manifest, err := ociClient.GetManifest(ctx, nsAddr, tag)
			if err != nil {
				propagateOCIDistError(err, resp)
				return
			}

			if mt := manifest.Config.MediaType; mt != "application/vnd.hashicorp.terraform-provider.config.v1+json" {
				logger.Printf("artifact %s %s has unsupported media type %s", nsAddr, tag, mt)
				resp.WriteHeader(406)
				return
			}

			type RespArchive struct {
				URL    string   `json:"url"`
				Hashes []string `json:"hashes,omitempty"`
			}
			type RespJSON struct {
				Archives map[string]RespArchive `json:"archives"`
			}
			respJSON := RespJSON{make(map[string]RespArchive)}

			for _, meta := range manifest.Layers {
				if meta.MediaType != "application/vnd.hashicorp.terraform.provider-package+zip" {
					continue // ignore any layer types other than our own
				}
				supportedPlatformsRaw, _ := meta.Annotations["io.terraform.target-platforms"].(string)
				if supportedPlatformsRaw == "" {
					continue // all packages should indicate which platforms they support
				}
				var downloadURL *url.URL
				if cfg.ProxyPackages {
					downloadURL = req.URL.JoinPath("../download")
					authHeader := req.Header.Get("authorization")
					var buf bytes.Buffer
					fmt.Fprintf(&buf, "%s:%s", meta.Digest.String(), authHeader)
					secret, err := secreter.Wrap(buf.Bytes())
					if err != nil {
						logger.Printf("failed to generate download authentication string: %s", err)
						resp.WriteHeader(500)
						return
					}
					downloadURL.RawQuery = secret
				} else {
					downloadURL = ociClient.BlobURL(nsAddr, meta.Digest)
				}
				respArchive := RespArchive{
					URL: downloadURL.String(),
				}
				if meta.Digest.Algorithm() == "sha256" {
					// Terraform's own "zh" ("ziphash") hashing scheme happens to
					// be exactly compatible with OCI Distribution's sha256
					// scheme, aside from the prefix, so we'll include this
					// to help the client do an integrity check on what it
					// has downloaded.
					respArchive.Hashes = []string{"zh:" + meta.Digest.Encoded()}
				}
				for _, platform := range strings.Split(supportedPlatformsRaw, ",") {
					respJSON.Archives[platform] = respArchive
				}
			}

			respBytes, err := json.Marshal(respJSON)
			if err != nil {
				logger.Printf("failed to serialize JSON response: %s", err)
				resp.WriteHeader(500)
				return
			}
			resp.Header().Set("Content-Length", strconv.FormatInt(int64(len(respBytes)), 10))
			resp.Header().Set("Content-Type", "application/json")
			resp.WriteHeader(200)
			resp.Write(respBytes)
			return
		}
	}
}

func ociDistNamespaceFromPathSegments(prefix ocidist.Namespace, segs []string) (ocidist.Namespace, error) {
	if len(segs) == 0 {
		return nil, fmt.Errorf("must provide at least one path segment")
	}
	ret := make([]ocidist.NamespacePart, len(segs))
	for i, seg := range segs {
		part, err := ocidist.ParseNamespacePart(seg)
		if err != nil {
			return nil, fmt.Errorf("segment %d: %w", i, err)
		}
		ret[i] = part
	}
	return prefix.Append(ret...), nil
}

func propagateOCIDistError(err error, resp http.ResponseWriter) {
	switch err {
	case ocidist.ErrBadGateway:
		resp.WriteHeader(502)
	case ocidist.ErrUnauthorized:
		resp.WriteHeader(401)
	case ocidist.ErrTimeout:
		resp.WriteHeader(504)
	}

	switch err.(type) {
	case ocidist.NotFoundError:
		resp.WriteHeader(404)
	case ocidist.RequestError:
		resp.WriteHeader(502)
	default:
		resp.WriteHeader(502)
	}
}
