// Package querysecret is a helper for situations where we need to pack a
// secret message into the query string of a URL, typically in order to
// propagate authentication from an authenticated request to a subsequent
// unauthenticated request in one of Terraform's protocols.
package querysecret

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"time"

	"golang.org/x/crypto/nacl/secretbox"
)

const nonceLength = 24

// Secreter is an object that can encrypt and decrypt query string secrets.
type Secreter struct {
	// randReader is a reader from a cryptographically secure random number
	// generator.
	randReader io.Reader

	// secretKey is the secret key used to protect the messages. This value
	// should be protected from access by anyone who wouldn't normally have
	// access to watch whatever data is being smuggled in the query string.
	secretKey [32]byte
}

// NewSecreter constructs and returns a new [Secreter] using the default
// random reader from the crypto/rand package.
func NewSecreter(secretKey [32]byte) *Secreter {
	return NewSecreterWithRand(secretKey, rand.Reader)
}

// NewSecreterWithRand is like [NewSecreter] but additionally allows providing
// your own random byte reader.
//
// The reader must represent a random number generator suitable for
// cryptographic use.
func NewSecreterWithRand(secretKey [32]byte, randReader io.Reader) *Secreter {
	return &Secreter{
		randReader: randReader,
		secretKey:  secretKey,
	}
}

// Wrap encrypts the given message and returns a string that uses the
// URL-oriented base64 alpbabet to represent both the message and some
// additonal overhead used to authenticate it.
func (s *Secreter) Wrap(msg []byte) (string, error) {
	var nonce [24]byte
	n, err := s.randReader.Read(nonce[:])
	if err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}
	if n != nonceLength {
		return "", fmt.Errorf("nonce has incorrect length")
	}

	wrapped := make([]byte, nonceLength, nonceLength+len(msg)+8+secretbox.Overhead)
	copy(wrapped, nonce[:])

	// A secret message is valid for three minutes after being generated
	expiration := time.Now().Unix() + 180
	var buf bytes.Buffer
	buf.Grow(8)
	binary.Write(&buf, binary.BigEndian, expiration)
	fullMsg := make([]byte, 0, len(msg)+8)
	fullMsg = append(fullMsg, buf.Bytes()...)
	fullMsg = append(fullMsg, msg...)

	wrapped = secretbox.Seal(wrapped, fullMsg, &nonce, &s.secretKey)
	return base64.URLEncoding.EncodeToString(wrapped), nil
}

// Unwrap takes a result from an earlier call to [Secreter.Wrap] on a Secreter
// with the same key as the receiver and returns the message wrapped inside.
func (s *Secreter) Unwrap(wrapped string) ([]byte, error) {
	rawLen := base64.URLEncoding.DecodedLen(len(wrapped))
	if rawLen < (nonceLength + secretbox.Overhead) {
		return nil, fmt.Errorf("message too short")
	}
	raw, err := base64.URLEncoding.DecodeString(wrapped)
	if err != nil {
		return nil, fmt.Errorf("invalid base64 encoding")
	}
	var nonce [24]byte
	copy(nonce[:], raw)
	raw = raw[24:]

	ret := make([]byte, 0, len(raw)-secretbox.Overhead)
	ret, ok := secretbox.Open(ret, raw, &nonce, &s.secretKey)
	if !ok {
		return nil, fmt.Errorf("decryption error")
	}

	r := bytes.NewReader(ret)
	var expirationUnix int64
	err = binary.Read(r, binary.BigEndian, &expirationUnix)
	if err != nil {
		return nil, fmt.Errorf("missing expiration time")
	}
	ret = ret[8:]
	expiration := time.Unix(expirationUnix, 0)
	if now := time.Now(); now.After(expiration) {
		return nil, fmt.Errorf("message has expired")
	}

	return ret, nil
}
