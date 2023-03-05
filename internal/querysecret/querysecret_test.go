package querysecret

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestSecreter(t *testing.T) {
	var key [32]byte
	n, err := rand.Read(key[:])
	if err != nil || n != 32 {
		t.Fatal("failed to generate key")
	}
	t.Logf("test key is %#v", key)

	s := NewSecreter(key)

	msg := []byte("hello!")
	qsArg, err := s.Wrap(msg)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("query string argument is %q", qsArg)

	got, err := s.Unwrap(qsArg)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, msg) {
		t.Error("result does not match input")
	}
}
