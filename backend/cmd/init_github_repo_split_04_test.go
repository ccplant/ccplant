package cmd

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
)

func createTempPEMFile(privateKey *rsa.PrivateKey) (string, error) {
	tmpFile, err := os.CreateTemp("", "test-key-*.pem")
	if err != nil {
		return "", err
	}
	defer func() { _ = tmpFile.Close() }()

	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	privateKeyPEM := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyBytes,
	}

	if err := pem.Encode(tmpFile, privateKeyPEM); err != nil {
		return "", err
	}

	return tmpFile.Name(), nil
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && findIndex(s, substr) >= 0
}

func findIndex(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
