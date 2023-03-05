package server

import (
	"context"
	"net/http"
)

type contextKey string

const remoteAddrContextKey = contextKey("remoteAddr")
const originalReq = contextKey("originalReq")

func contextWithOriginalReq(parentCtx context.Context, req *http.Request) context.Context {
	return context.WithValue(parentCtx, originalReq, req)
}

func contextOriginalReq(ctx context.Context) *http.Request {
	ret := ctx.Value(originalReq)
	if ret == nil {
		return nil
	}
	return ret.(*http.Request)
}
