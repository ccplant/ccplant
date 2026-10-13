// update-keygen creates base64-encoded Ed25519 keys for the release workflow.
// Keep the private output only in the CCPLANT_UPDATE_SIGNING_KEY GitHub secret.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
)

func main() {
	privatePath := flag.String("private-out", "", "private-key output path")
	publicPath := flag.String("public-out", "", "public-key output path")
	flag.Parse()
	if *privatePath == "" || *publicPath == "" {
		fatal("--private-out and --public-out are required")
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fatal("generate key: %v", err)
	}
	if err := os.WriteFile(*privatePath, []byte(base64.StdEncoding.EncodeToString(privateKey)+"\n"), 0o600); err != nil {
		fatal("write private key: %v", err)
	}
	if err := os.WriteFile(*publicPath, []byte(base64.StdEncoding.EncodeToString(publicKey)+"\n"), 0o600); err != nil {
		_ = os.Remove(*privatePath)
		fatal("write public key: %v", err)
	}
}

func fatal(format string, values ...interface{}) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", values...)
	os.Exit(1)
}
