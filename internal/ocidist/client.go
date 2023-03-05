package ocidist

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Client is a client for the subset of the OCI distribution protocol that's
// relevant for this application.
//
// This is not a general-purpose client for that protocol, and intentionally
// skips all protocol features that are not relevant to implementing
// Terraform's registry and mirror protocols in terms of an OCI distribution
// registry.
type Client struct {
	baseURL    *url.URL
	prepareReq []func(req *http.Request) error
	rawClient  *http.Client
}

// NewClient constructs and returns a new [Client] that will talk to an OCI
// distribution registry at the given base URL.
//
// The given URL must use either the "http" or "https" scheme, or this function
// will panic. The URL must not include a user info portion, because we handle
// authentication separately; this function will panic if the given URL has
// user information. Use [AssertValidRegistryURL] to test whether a
// user-provided URL would be accepted by this function without panicking.
func NewClient(baseURL *url.URL) *Client {
	if err := AssertValidRegistryURL(baseURL); err != nil {
		panic(err.Error)
	}
	return &Client{
		baseURL:   baseURL,
		rawClient: http.DefaultClient,
	}
}

// NewClientWithRoundtripper constructs and returns a new [Client], with the
// same rules as [NewClient] but with a custom HTTP round-tripper
// implementation.
func NewClientWithRoundTripper(baseURL *url.URL, rt http.RoundTripper) *Client {
	client := NewClient(baseURL)
	client.rawClient = &http.Client{
		Transport: rt,
	}
	return client
}

// AssertValidRegistryURL checks whether the given URL is acceptable to pass
// to [NewClient], return an error describing a problem if not.
//
// If the result is nil then [NewClient] is guaranteed to accept the same URL
// without panicking, although that doesn't guarantee that the URL will actually
// work when it comes to making real API requests.
func AssertValidRegistryURL(baseURL *url.URL) error {
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return fmt.Errorf("must use scheme \"https\" or \"http\", not %q", baseURL.Scheme)
	}
	if baseURL.User != nil {
		return fmt.Errorf("must not include a user information portion")
	}
	return nil
}

// AddPrepareRequest provides a function that the client will call just before
// making any HTTP request, giving an opportunity to add authentication
// credentials or other context.
//
// The request-preparation function must not modify the request in any way that
// would change the meaning of what is being requested or what format the
// response would be in. For example, it would be acceptable to set the
// Authorization header or the User-Agent header, but it would not be acceptable
// to modify the Accept header or other similar content-negotiation-related
// headers.
//
// This must not be called concurrently with any other method of the same
// client object. Typically it would be called only during the initial setup of
// the client.
func (c *Client) AddPrepareRequest(cb func(req *http.Request) error) {
	c.prepareReq = append(c.prepareReq, cb)
}

// CheckAPISupport attempts to detect whether the client's configured base
// URL is an implementation of the OCI Distribution specification.
//
// This is just a heuristic to help the system fail early if given an invalid
// URL. If the error is not nil then this is either not an OCI Distribution
// server or the provided authentication credentials are invalid.
//
// If the error is nil then the base URL _might_ be a valid OCI Distribution
// implementation, but we can't be sure until we actually try to request data
// from it.
func (c *Client) CheckAPISupport(ctx context.Context) error {
	req, err := c.newRequest(ctx, "GET", "v2/")
	if err != nil {
		return fmt.Errorf("failed to prepare request: %s", err)
	}
	resp, err := c.rawClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %s", err)
	}
	resp.Body.Close()
	return nil
}

// GetNamespaceTags returns all of the tags that are available for the
// given namespace in the target registry.
//
// If the server returns any tag names that aren't valid reference strings per
// the OCI Distribution specification then this function will silently discard
// them and return only the valid subset.
func (c *Client) GetNamespaceTags(ctx context.Context, ns Namespace) ([]Reference, error) {
	req, err := c.newRequest(ctx, "GET", "v2", ns.String(), "tags", "list")
	if err != nil {
		return nil, fmt.Errorf("failed to prepare request: %s", err)
	}

	type RespBody struct {
		Tags []string `json:"tags"`
	}
	var respBody RespBody
	err = c.doRequestJSONResp(req, &respBody)
	if err != nil {
		return nil, err
	}

	ret := make([]Reference, 0, len(respBody.Tags))
	for _, rawTag := range respBody.Tags {
		ref, err := ParseReference(rawTag)
		if err != nil {
			continue
		}
		ret = append(ret, ref)
	}
	return ret, nil
}

// GetManifest returns the manifest for the given reference associated with
// the given namespace.
func (c *Client) GetManifest(ctx context.Context, ns Namespace, ref Reference) (*Manifest, error) {
	req, err := c.newRequest(ctx, "GET", "v2", ns.String(), "manifests", ref.String())
	if err != nil {
		return nil, fmt.Errorf("failed to prepare request: %s", err)
	}
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json, application/vnd.oci.artifact.manifest.v1+json")

	respBody := &Manifest{}
	err = c.doRequestJSONResp(req, respBody)
	if err != nil {
		return nil, err
	}
	if respBody.SchemaVersion != 2 {
		return nil, fmt.Errorf("unsupported manifest schema version %#v", respBody.SchemaVersion)
	}
	return respBody, nil
}

// BlobURL returns the full URL for retrieving the content of the object with
// the given digest belonging to the given namespace.
//
// Where possible the registry/mirror server implementations point directly to
// the blob endpoint when describing the location of the actual artifact,
// because that then allows Terraform CLI to pull the data directly from the
// registry, since no API translation is needed for that step.
func (c *Client) BlobURL(ns Namespace, digest Digest) *url.URL {
	return c.baseURL.JoinPath("v2", ns.String(), "blobs", digest.String())
}

func (c *Client) newRequest(ctx context.Context, method string, urlParts ...string) (*http.Request, error) {
	u := c.baseURL.JoinPath(urlParts...)
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	for _, cb := range c.prepareReq {
		err := cb(req)
		if err != nil {
			return nil, err
		}
	}
	return req, nil
}

func (c *Client) newRequestWithBody(ctx context.Context, method string, body io.Reader, urlParts ...string) (*http.Request, error) {
	u := c.baseURL.JoinPath(urlParts...)
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), body)
	if err != nil {
		return nil, err
	}

	for _, cb := range c.prepareReq {
		err := cb(req)
		if err != nil {
			return nil, err
		}
	}
	return req, nil
}

func (c *Client) doRequestJSONResp(req *http.Request, into any) error {
	resp, err := c.rawClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %s", err)
	}
	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)
	err = dec.Decode(into)
	if err != nil {
		return fmt.Errorf("response is not in the expected format: %s", err)
	}
	// NOTE: If there's anything trailing after the JSON object then we'll
	// just ignore it. That would not be valid per the OCI Distribution spec
	// but we'll tolerate it anyway because it doesn't hurt and is easier.
	return nil
}
