package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/apparentlymart/oci-distribution-terraform-registry/internal/config"
	"github.com/apparentlymart/oci-distribution-terraform-registry/internal/logging"
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

	advertiseHandler := func(resp http.ResponseWriter, req *http.Request) {
		// TODO: A more elaborate page
		content := "<!DOCTYPE html><html><title>Provider Mirror</title><body>This is a Terraform provider mirror.</body></html>"
		resp.Header().Set("Content-Type", "text/html; charset=utf-8")
		resp.Header().Set("Content-Length", strconv.FormatInt(int64(len(content)), 10))
		resp.WriteHeader(200)
		resp.Write([]byte(content))
	}

	return prefix, func(resp http.ResponseWriter, req *http.Request) {
		_, done := logging.ContextLoggerRequest(req.Context(), "request to provider mirror: %s", req.URL)
		defer done()

		pathParts := strings.Split(req.URL.EscapedPath(), "/")
		if len(pathParts) == 3 && pathParts[2] == "" {
			// This is a request to our root, which isn't used as part of the
			// mirror protocol and so we'll produce a HTML page advertising
			// the mirror instead.
			advertiseHandler(resp, req)
			return
		}

		// TODO: Actually implement the mirror protocol.
		resp.WriteHeader(404)
	}
}

type contextKey string

const remoteAddrContextKey = contextKey("remoteAddr")
