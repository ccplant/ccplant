package kvstore

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type branchKMSProvider interface {
	Name() string
	Encrypt(context.Context, string, []byte, map[string]string) ([]byte, error)
	Decrypt(context.Context, string, []byte, map[string]string) ([]byte, error)
}

type awsBranchKMSProvider struct{ client kmsAPI }

func (p *awsBranchKMSProvider) Name() string { return "aws-kms" }

func (p *awsBranchKMSProvider) Encrypt(ctx context.Context, keyRef string, plaintext []byte, aad map[string]string) ([]byte, error) {
	output, err := p.client.Encrypt(ctx, &kms.EncryptInput{
		KeyId: aws.String(keyRef), Plaintext: plaintext, EncryptionContext: aad,
	})
	if err != nil {
		return nil, fmt.Errorf("encrypt branch key with AWS KMS: %w", err)
	}
	if output == nil || len(output.CiphertextBlob) == 0 {
		return nil, errors.New("AWS KMS returned empty branch key ciphertext")
	}
	return output.CiphertextBlob, nil
}

func (p *awsBranchKMSProvider) Decrypt(ctx context.Context, keyRef string, ciphertext []byte, aad map[string]string) ([]byte, error) {
	output, err := p.client.Decrypt(ctx, &kms.DecryptInput{
		KeyId: aws.String(keyRef), CiphertextBlob: ciphertext, EncryptionContext: aad,
	})
	if err != nil || output == nil || len(output.Plaintext) != dataKeySize {
		if output != nil {
			clear(output.Plaintext)
		}
		return nil, ErrDecrypt
	}
	return output.Plaintext, nil
}

type cloudKMSProvider struct{ client *http.Client }

func newCloudKMSProvider(ctx context.Context) (*cloudKMSProvider, error) {
	credentials, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("load Google Cloud application default credentials: %w", err)
	}
	return &cloudKMSProvider{client: oauth2.NewClient(ctx, credentials.TokenSource)}, nil
}

func (p *cloudKMSProvider) Name() string { return "cloud-kms" }

func (p *cloudKMSProvider) Encrypt(ctx context.Context, keyRef string, plaintext []byte, aad map[string]string) ([]byte, error) {
	request := map[string]string{
		"plaintext":                   base64.StdEncoding.EncodeToString(plaintext),
		"additionalAuthenticatedData": base64.StdEncoding.EncodeToString(branchAAD(aad)),
	}
	var response struct {
		Ciphertext string `json:"ciphertext"`
	}
	if err := p.call(ctx, keyRef, "encrypt", request, &response); err != nil {
		return nil, err
	}
	ciphertext, err := base64.StdEncoding.Strict().DecodeString(response.Ciphertext)
	if err != nil || len(ciphertext) == 0 {
		return nil, errors.New("Cloud KMS returned invalid branch key ciphertext")
	}
	return ciphertext, nil
}

func (p *cloudKMSProvider) Decrypt(ctx context.Context, keyRef string, ciphertext []byte, aad map[string]string) ([]byte, error) {
	request := map[string]string{
		"ciphertext":                  base64.StdEncoding.EncodeToString(ciphertext),
		"additionalAuthenticatedData": base64.StdEncoding.EncodeToString(branchAAD(aad)),
	}
	var response struct {
		Plaintext string `json:"plaintext"`
	}
	if err := p.call(ctx, keyRef, "decrypt", request, &response); err != nil {
		return nil, ErrDecrypt
	}
	plaintext, err := base64.StdEncoding.Strict().DecodeString(response.Plaintext)
	if err != nil || len(plaintext) != dataKeySize {
		clear(plaintext)
		return nil, ErrDecrypt
	}
	return plaintext, nil
}

func (p *cloudKMSProvider) call(ctx context.Context, keyRef, operation string, input, output any) error {
	keyRef = strings.TrimPrefix(strings.TrimSpace(keyRef), "/")
	if !strings.HasPrefix(keyRef, "projects/") || strings.ContainsAny(keyRef, "?#:") {
		return errors.New("invalid Cloud KMS key resource name")
	}
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	url := "https://cloudkms.googleapis.com/v1/" + keyRef + ":" + operation
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("call Cloud KMS %s: %w", operation, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("Cloud KMS %s returned HTTP %d", operation, resp.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("decode Cloud KMS %s response: %w", operation, err)
	}
	return nil
}

func branchAAD(values map[string]string) []byte {
	encoded, _ := json.Marshal(values)
	return encoded
}
