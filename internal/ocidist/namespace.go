package ocidist

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Namespace represents a sequence of slash-separated parts used as the
// identifier for a "namespace" in an OCI distribution registry.
//
// Although not enforcable by the Go compiler, a valid Namespace value must
// always have at least one part, and all of the parts must themselves be
// valid per the definition of [NamespacePart]. Use [ParseNamespace] to
// guarantee a valid Namespace value.
type Namespace []NamespacePart

// NamespacePart represents one of the possibly-several slash-separated parts
// in an OCI distribution registry namespace.
//
// Although not enforcable by the Go compiler, values of this type must always
// be valid namespace parts as defined in the OCI Distribution specification,
// which means they must match the following regular expression pattern:
//
//	[a-z0-9]+([._-][a-z0-9]+)*
//
// Use [ParseNamespacePart] to guarantee a valid value.
type NamespacePart string

// Reference represents a reference string used to identify a particular
// tag or other reference in an OCI distribution registry.
//
// Although not enforcable by the Go compiler, values of this type must
// always be valid reference strings as defined in the OCI Distribution
// specification, which means they must match the following regular expression
// pattern:
//
//	[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}
//
// Use [ParseReference] to guarantee a valid value.
type Reference string

// Digest represents an object digest, formatted as a hash algorithm followed
// by a colon and then a sequence of characters representing a result of the
// chosen algorithm.
//
// Although not enforceable by the Go compiler, values of this type must
// always be valid checksum syntax as defined by trhe OCI Distribution
// specification.
type Digest string

// ParseNamespace parses a string in the format defined in the OCI Distribution
// specification into a [Namespace] value, or returns an error if the given
// string does not use valid syntax.
func ParseNamespace(s string) (Namespace, error) {
	if len(s) == 0 {
		return nil, fmt.Errorf("must include at least one namespace part")
	}
	parts := strings.Split(s, "/")
	ret := make(Namespace, len(parts))
	for i, raw := range parts {
		var err error
		ret[i], err = ParseNamespacePart(raw)
		if err != nil {
			return nil, fmt.Errorf("part %d is invalid: %s", i+1, err)
		}
	}
	return ret, nil
}

func MustParseNamespace(s string) Namespace {
	ns, err := ParseNamespace(s)
	if err != nil {
		panic(err)
	}
	return ns
}

func ParseNamespacePart(s string) (NamespacePart, error) {
	if !namespacePartRe.MatchString(s) {
		return "", fmt.Errorf("must consist of one or more sequences of lowercase latin letters and digits separated by individual periods, underscores, or dashes")
	}
	return NamespacePart(s), nil
}

func MustParseNamespacePart(s string) NamespacePart {
	np, err := ParseNamespacePart(s)
	if err != nil {
		panic(err)
	}
	return np
}

func ParseReference(s string) (Reference, error) {
	if !referenceRe.MatchString(s) {
		return "", fmt.Errorf("must consist of a latin letter, digit, or underscore, followed by up to 127 more latin letters, digits, underscores, dashes, or dots")
	}
	return Reference(s), nil
}

func MustParseReference(s string) Reference {
	r, err := ParseReference(s)
	if err != nil {
		panic(err)
	}
	return r
}

func ParseDigest(s string) (Digest, error) {
	colon := strings.IndexByte(s, ':')
	if colon < 1 {
		return "", fmt.Errorf("must be a hash algorithm followed by a colon and then the hash result")
	}
	algo := s[:colon]
	enc := s[colon+1:]
	if !digestAlgorithmRe.MatchString(algo) {
		return "", fmt.Errorf("invalid hash algorithm")
	}
	if !digestEncodedRe.MatchString(enc) {
		return "", fmt.Errorf("invalid encoded digest result")
	}
	return Digest(s), nil
}

var _ json.Unmarshaler = (*Digest)(nil)

func (ns Namespace) String() string {
	var buf strings.Builder
	for i, part := range ns {
		if i > 0 {
			buf.WriteByte('/')
		}
		buf.WriteString(string(part))
	}
	return buf.String()
}

// Append builds a new [Namespace] by appending another part to an existing
// [Namespace].
func (ns Namespace) Append(part NamespacePart) Namespace {
	ret := make(Namespace, len(ns)+1)
	copy(ret, ns)
	return append(ret, part)
}

func (np NamespacePart) String() string {
	return string(np)
}

func (r Reference) String() string {
	return string(r)
}

func (d Digest) String() string {
	return string(d)
}

func (d Digest) Algorithm() string {
	colon := strings.IndexByte(string(d), ':')
	return string(d)[:colon]
}

func (d Digest) Encoded() string {
	colon := strings.IndexByte(string(d), ':')
	return string(d)[colon+1:]
}

func (d *Digest) UnmarshalJSON(src []byte) error {
	var raw string
	err := json.Unmarshal(src, &raw)
	if err != nil {
		return fmt.Errorf("must be a string")
	}
	v, err := ParseDigest(raw)
	if err != nil {
		return err
	}
	*d = v
	return nil
}

var namespacePartRe = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*$`)
var referenceRe = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$`)
var digestAlgorithmRe = regexp.MustCompile(`^[a-z0-9]+([+._-][a-z0-9]+)*$`)
var digestEncodedRe = regexp.MustCompile(`^[a-zA-Z0-9=_-]+$`)
