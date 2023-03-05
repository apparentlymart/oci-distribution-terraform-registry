package ocidist

import (
	"encoding/json"
	"fmt"
)

type staticError string

func (err staticError) Error() string {
	return string(err)
}

const ErrUnauthorized = staticError("unauthorized")
const ErrTimeout = staticError("timeout")
const ErrBadGateway = staticError("invalid response from backend server")

type NotFoundError struct {
	JSONDesc json.RawMessage
}

func (err NotFoundError) Error() string {
	return "not found"
}

type RequestError struct {
	Wrapped error
}

func (err RequestError) Error() string {
	return fmt.Sprintf("request failed: %s", err.Wrapped)
}

func (err RequestError) Unwrap() error {
	return err.Wrapped
}
