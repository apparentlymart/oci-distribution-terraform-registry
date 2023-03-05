package logging

import (
	"context"
	"fmt"
	"log"
)

func ContextWithLogger(parentCtx context.Context, logger *log.Logger) context.Context {
	return context.WithValue(parentCtx, loggerContextKey, logger)
}

func ContextLogger(ctx context.Context) *log.Logger {
	logger := ctx.Value(loggerContextKey).(*log.Logger)
	if logger == nil {
		logger = log.Default()
	}
	return logger
}

func ContextLoggerRequest(ctx context.Context, f string, args ...any) (*log.Logger, func()) {
	logger := ContextLogger(ctx)
	reqType := fmt.Sprintf(f, args...)
	logger.Print("BEGIN ", reqType)
	return logger, func() {
		logger.Print("END ", reqType)
	}
}

type contextKey string

const loggerContextKey = contextKey("logger")
