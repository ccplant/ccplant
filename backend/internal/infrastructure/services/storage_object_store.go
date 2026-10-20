package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

// StorageObjectStore holds immutable, content-addressed file blobs. Metadata
// and last-write-wins serialization remain in StorageRepository.
type StorageObjectStore interface {
	Put(context.Context, string, string, []byte) error
	Get(context.Context, string, string) (io.ReadCloser, error)
}

type s3StorageObjectStore struct {
	client         *s3.Client
	bucket, prefix string
}

// NewStorageObjectStore uses the configured session-persistence S3 service as
// the first storage provider. A dedicated provider registry can replace this
// adapter without changing the public storage API.
func NewStorageObjectStore(ctx context.Context, cfg *config.S3StorageConfig) (StorageObjectStore, error) {
	if cfg == nil || cfg.Bucket == "" {
		return nil, nil
	}
	options := []func(*awsconfig.LoadOptions) error{}
	if cfg.Region != "" {
		options = append(options, awsconfig.WithRegion(cfg.Region))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, err
	}
	s3Options := []func(*s3.Options){}
	if cfg.Endpoint != "" {
		s3Options = append(s3Options, func(options *s3.Options) {
			options.BaseEndpoint = aws.String(cfg.Endpoint)
			options.UsePathStyle = true
		})
	}
	prefix := strings.Trim(cfg.Prefix, "/")
	if prefix != "" {
		prefix += "/"
	}
	prefix += "storages/"
	return &s3StorageObjectStore{client: s3.NewFromConfig(awsCfg, s3Options...), bucket: cfg.Bucket, prefix: prefix}, nil
}

func (s *s3StorageObjectStore) key(storageID, digest string) (string, error) {
	if storageID == "" || strings.ContainsAny(storageID, `/\\`) {
		return "", fmt.Errorf("invalid storage id")
	}
	if len(digest) != 64 || strings.Trim(digest, "0123456789abcdef") != "" {
		return "", fmt.Errorf("invalid storage digest")
	}
	return s.prefix + storageID + "/blobs/sha256/" + digest, nil
}

func (s *s3StorageObjectStore) Put(ctx context.Context, storageID, digest string, content []byte) error {
	key, err := s.key(storageID, digest)
	if err != nil {
		return err
	}
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), Body: bytes.NewReader(content),
		ContentLength: aws.Int64(int64(len(content))), ContentType: aws.String("application/octet-stream"),
	})
	return err
}

func (s *s3StorageObjectStore) Get(ctx context.Context, storageID, digest string) (io.ReadCloser, error) {
	key, err := s.key(storageID, digest)
	if err != nil {
		return nil, err
	}
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		var missing *types.NoSuchKey
		if errors.As(err, &missing) {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	return output.Body, nil
}
