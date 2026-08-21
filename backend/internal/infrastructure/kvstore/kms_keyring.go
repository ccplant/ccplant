package kvstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
)

type kmsAPI interface {
	GenerateDataKey(context.Context, *kms.GenerateDataKeyInput, ...func(*kms.Options)) (*kms.GenerateDataKeyOutput, error)
	Encrypt(context.Context, *kms.EncryptInput, ...func(*kms.Options)) (*kms.EncryptOutput, error)
	Decrypt(context.Context, *kms.DecryptInput, ...func(*kms.Options)) (*kms.DecryptOutput, error)
}

type KMSKeyring struct {
	activeID string
	keys     map[string]string
	client   kmsAPI
}

func NewKMSKeyring(ctx context.Context, activeID, region string, keys map[string]string) (*KMSKeyring, error) {
	activeID = strings.TrimSpace(activeID)
	if activeID == "" || strings.TrimSpace(region) == "" {
		return nil, errors.New("active KV encryption key ID and KMS region are required")
	}
	if _, ok := keys[activeID]; !ok {
		return nil, fmt.Errorf("active KV encryption key %q is not in the KMS keyring", activeID)
	}
	for id, keyARN := range keys {
		if id == "" || len(id) > 128 || strings.TrimSpace(keyARN) == "" {
			return nil, fmt.Errorf("invalid KMS keyring entry %q", id)
		}
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load AWS configuration for KV encryption: %w", err)
	}
	return &KMSKeyring{activeID: activeID, keys: keys, client: kms.NewFromConfig(cfg)}, nil
}

func (k *KMSKeyring) ActiveKeyID() string { return k.activeID }

func (k *KMSKeyring) GenerateDataKey(ctx context.Context, record Record) ([]byte, []byte, error) {
	output, err := k.client.GenerateDataKey(ctx, &kms.GenerateDataKeyInput{
		KeyId:             aws.String(k.keys[k.activeID]),
		KeySpec:           types.DataKeySpecAes256,
		EncryptionContext: kmsEncryptionContext(record),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("generate KMS data key: %w", err)
	}
	if output == nil || len(output.Plaintext) != dataKeySize || len(output.CiphertextBlob) == 0 {
		if output == nil {
			return nil, nil, errors.New("KMS returned no data key")
		}
		clear(output.Plaintext)
		return nil, nil, errors.New("KMS returned an invalid data key")
	}
	return output.Plaintext, output.CiphertextBlob, nil
}

func (k *KMSKeyring) WrapDataKey(ctx context.Context, keyID string, dek []byte, record Record) ([]byte, error) {
	keyARN, ok := k.keys[keyID]
	if !ok {
		return nil, ErrDecrypt
	}
	output, err := k.client.Encrypt(ctx, &kms.EncryptInput{
		KeyId:             aws.String(keyARN),
		Plaintext:         dek,
		EncryptionContext: kmsEncryptionContext(record),
	})
	if err != nil {
		return nil, fmt.Errorf("wrap KV data key with KMS: %w", err)
	}
	return output.CiphertextBlob, nil
}

func (k *KMSKeyring) UnwrapDataKey(ctx context.Context, keyID string, wrapped []byte, record Record) ([]byte, error) {
	keyARN, ok := k.keys[keyID]
	if !ok {
		return nil, ErrDecrypt
	}
	output, err := k.client.Decrypt(ctx, &kms.DecryptInput{
		KeyId:             aws.String(keyARN),
		CiphertextBlob:    wrapped,
		EncryptionContext: kmsEncryptionContext(record),
	})
	if err != nil || output == nil || len(output.Plaintext) != dataKeySize {
		if output != nil {
			clear(output.Plaintext)
		}
		return nil, ErrDecrypt
	}
	return output.Plaintext, nil
}

func kmsEncryptionContext(record Record) map[string]string {
	return map[string]string{
		"application": "agentapi-kv",
		"kind":        string(record.Kind),
		"namespace":   record.Namespace,
		"key":         record.Key,
	}
}
