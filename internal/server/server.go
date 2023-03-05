package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/apparentlymart/oci-distribution-terraform-registry/internal/config"
	"github.com/apparentlymart/oci-distribution-terraform-registry/internal/logging"
)

func Run(ctx context.Context, config *config.Config) error {
	mux := http.NewServeMux()

	for _, mirrorSvc := range config.ProviderMirrors {
		serviceName := mirrorSvc.Name
		prefix := "/" + mirrorSvc.Name + "/"
		mux.HandleFunc(prefix, func(resp http.ResponseWriter, req *http.Request) {
			_, done := logging.ContextLoggerRequest(req.Context(), "request to provider mirror %q", serviceName)
			defer done()
			resp.WriteHeader(500)
		})
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

type contextKey string

const remoteAddrContextKey = contextKey("remoteAddr")
