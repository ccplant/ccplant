package buildinfo

// Version is replaced at image build time with the immutable image version.
var Version = "dev"

// Distribution identifies the component that owns this binary's lifecycle.
// Only github-release binaries may replace themselves.
var Distribution = "dev"

// UpdatePublicKey is the base64-encoded Ed25519 public key used to verify
// release update manifests. Release builds replace it with the production key.
var UpdatePublicKey = ""
