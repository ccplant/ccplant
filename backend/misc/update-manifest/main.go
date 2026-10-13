package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type asset struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type manifest struct {
	SchemaVersion int              `json:"schema_version"`
	Version       string           `json:"version"`
	PublishedAt   time.Time        `json:"published_at"`
	Assets        map[string]asset `json:"assets"`
}

func main() {
	version := flag.String("version", "", "release version such as v1.2.3")
	directory := flag.String("dist", "dist", "directory containing release archives")
	flag.Parse()
	if *version == "" {
		fatal("--version is required")
	}
	keyData, err := base64.StdEncoding.DecodeString(os.Getenv("CCPLANT_UPDATE_SIGNING_KEY"))
	if err != nil || len(keyData) != ed25519.PrivateKeySize {
		fatal("CCPLANT_UPDATE_SIGNING_KEY must contain a base64 Ed25519 private key")
	}
	publicKey, err := base64.StdEncoding.DecodeString(os.Getenv("CCPLANT_UPDATE_PUBLIC_KEY"))
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		fatal("CCPLANT_UPDATE_PUBLIC_KEY must contain a base64 Ed25519 public key")
	}
	derivedPublicKey := ed25519.PrivateKey(keyData).Public().(ed25519.PublicKey)
	if !ed25519.PublicKey(publicKey).Equal(derivedPublicKey) {
		fatal("update signing and public keys do not match")
	}
	files := map[string]string{
		"linux/amd64":  "ccplant_Linux_x86_64.tar.gz",
		"linux/arm64":  "ccplant_Linux_arm64.tar.gz",
		"darwin/arm64": "ccplant_Darwin_arm64.tar.gz",
	}
	document := manifest{SchemaVersion: 1, Version: *version, PublishedAt: time.Now().UTC().Truncate(time.Second), Assets: make(map[string]asset, len(files))}
	for platform, name := range files {
		data, readErr := os.ReadFile(filepath.Join(*directory, name))
		if readErr != nil {
			fatal("read %s: %v", name, readErr)
		}
		sum := sha256.Sum256(data)
		document.Assets[platform] = asset{Name: name, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))}
	}
	data, err := json.Marshal(document)
	if err != nil {
		fatal("encode manifest: %v", err)
	}
	manifestPath := filepath.Join(*directory, "ccplant-update-manifest.json")
	if err := os.WriteFile(manifestPath, append(data, '\n'), 0o644); err != nil {
		fatal("write manifest: %v", err)
	}
	// Sign the exact bytes written, including the trailing newline.
	signature := ed25519.Sign(ed25519.PrivateKey(keyData), append(data, '\n'))
	if err := os.WriteFile(manifestPath+".sig", []byte(base64.StdEncoding.EncodeToString(signature)+"\n"), 0o644); err != nil {
		fatal("write signature: %v", err)
	}
}

func fatal(format string, values ...interface{}) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", values...)
	os.Exit(1)
}
