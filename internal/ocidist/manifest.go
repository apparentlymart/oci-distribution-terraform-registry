package ocidist

// Manifest represents an OCI Distribution manifest, assuming schema version 2.
type Manifest struct {
	SchemaVersion int64          `json:"schemaVersion"`
	MediaType     string         `json:"mediaType"`
	Config        ObjectMeta     `json:"config"`
	Layers        []ObjectMeta   `json:"layers"`
	Annotations   map[string]any `json:"annotations"`
}

// ObjectMeta is the metadata for a content-addressable object such as a layer
// or config blob in a registry.
type ObjectMeta struct {
	MediaType   string         `json:"mediaType"`
	Digest      Digest         `json:"digest"`
	Size        int64          `json:"size"`
	Annotations map[string]any `json:"annotations"`
}
