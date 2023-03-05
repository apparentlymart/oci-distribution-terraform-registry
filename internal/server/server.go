package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/apparentlymart/go-versions/versions"
	"github.com/apparentlymart/oci-distribution-terraform-registry/internal/config"
	"github.com/apparentlymart/oci-distribution-terraform-registry/internal/logging"
	"github.com/apparentlymart/oci-distribution-terraform-registry/internal/ocidist"
)

func Run(ctx context.Context, config *config.Config) error {
	mux := http.NewServeMux()

	for _, mirrorSvc := range config.ProviderMirrors {
		mux.HandleFunc(providerMirrorHandler(mirrorSvc))
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

func providerMirrorHandler(cfg *config.ProviderMirror) (string, func(resp http.ResponseWriter, req *http.Request)) {
	serviceName := cfg.Name
	prefix := "/" + serviceName + "/"

	ociClient := ocidist.NewClient(cfg.OriginURL)
	userAgent := fmt.Sprintf("oci-distribution-terraform-registry (provider mirror %q)", "serviceName")
	ociClient.AddPrepareRequest(func(req *http.Request) error {
		req.Header.Set("User-Agent", userAgent)

		ctx := req.Context()
		originalReq := contextOriginalReq(ctx)
		if a := originalReq.Header.Get("authorization"); a != "" {
			// Pass through the Authorization header to the backend.
			req.Header.Set("Authorization", a)
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
		logger, done := logging.ContextLoggerRequest(req.Context(), "request to provider mirror: %s", req.URL)
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
		}
		selector := remainParts[0]
		if !strings.HasSuffix(selector, ".json") {
			// All selectors always have a .json suffix in the protocol
			resp.WriteHeader(404)
		}
		selector = selector[:len(selector)-5]

		ctx := contextWithOriginalReq(req.Context(), req)
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

		// TODO: Implement the rest of the protocol.
		resp.WriteHeader(404)
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
