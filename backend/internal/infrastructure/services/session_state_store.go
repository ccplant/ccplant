package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

// SessionStateStore is the backend-owned durable store for ACP snapshots.
type SessionStateStore interface {
	Save(context.Context, string, io.Reader) error
	Load(context.Context, string) (io.ReadCloser, error)
}

type volumeSessionStateStore struct{ root string }

func newVolumeSessionStateStore(root string) (SessionStateStore, error) {
	if root == "" {
		return nil, fmt.Errorf("session persistence path is required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &volumeSessionStateStore{root: root}, nil
}

func safeSessionStateID(id string) error {
	if id == "" || filepath.Base(id) != id || strings.ContainsAny(id, `/\\`) {
		return fmt.Errorf("invalid session state id")
	}
	return nil
}

func (s *volumeSessionStateStore) path(id string) (string, error) {
	if err := safeSessionStateID(id); err != nil {
		return "", err
	}
	return filepath.Join(s.root, id+".tar.gz"), nil
}
func (s *volumeSessionStateStore) Save(_ context.Context, id string, data io.Reader) error {
	p, err := s.path(id)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.root, id+"-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err = io.Copy(tmp, data); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, p)
}
func (s *volumeSessionStateStore) Load(_ context.Context, id string) (io.ReadCloser, error) {
	p, err := s.path(id)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

type sessionStateS3Client interface {
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}
type sessionStateS3Uploader interface {
	Upload(context.Context, *s3.PutObjectInput, ...func(*manager.Uploader)) (*manager.UploadOutput, error)
}
type s3SessionStateStore struct {
	client         sessionStateS3Client
	uploader       sessionStateS3Uploader
	bucket, prefix string
}

func newS3SessionStateStore(ctx context.Context, cfg *config.MemoryS3Config) (SessionStateStore, error) {
	if cfg == nil || cfg.Bucket == "" {
		return nil, fmt.Errorf("session persistence S3 bucket is required")
	}
	opts := []func(*awsconfig.LoadOptions) error{}
	if cfg.Region != "" {
		opts = append(opts, awsconfig.WithRegion(cfg.Region))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, err
	}
	s3opts := []func(*s3.Options){}
	if cfg.Endpoint != "" {
		s3opts = append(s3opts, func(o *s3.Options) { o.BaseEndpoint = aws.String(cfg.Endpoint); o.UsePathStyle = true })
	}
	prefix := cfg.Prefix
	if prefix == "" {
		prefix = "agentapi-sessions/"
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	client := s3.NewFromConfig(awsCfg, s3opts...)
	uploader := manager.NewUploader(client, func(u *manager.Uploader) {
		u.PartSize = 8 << 20
		u.Concurrency = 2
	})
	return &s3SessionStateStore{client: client, uploader: uploader, bucket: cfg.Bucket, prefix: prefix}, nil
}
func (s *s3SessionStateStore) key(id string) (string, error) {
	if err := safeSessionStateID(id); err != nil {
		return "", err
	}
	return s.prefix + id + ".tar.gz", nil
}
func (s *s3SessionStateStore) Save(ctx context.Context, id string, data io.Reader) error {
	k, err := s.key(id)
	if err != nil {
		return err
	}
	_, err = s.uploader.Upload(ctx, &s3.PutObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(k), Body: data, ContentType: aws.String("application/gzip")})
	return err
}
func (s *s3SessionStateStore) Load(ctx context.Context, id string) (io.ReadCloser, error) {
	k, err := s.key(id)
	if err != nil {
		return nil, err
	}
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(k)})
	if err != nil {
		var nsk *types.NoSuchKey
		if errors.As(err, &nsk) {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	return out.Body, nil
}

// NewSessionStateStore builds the configured backend. Empty backend disables persistence.
func NewSessionStateStore(ctx context.Context, cfg config.SessionPersistenceConfig) (SessionStateStore, error) {
	switch cfg.Backend {
	case "":
		return nil, nil
	case "volume":
		return newVolumeSessionStateStore(cfg.Path)
	case "s3":
		return newS3SessionStateStore(ctx, cfg.S3)
	default:
		return nil, fmt.Errorf("unsupported session persistence backend %q", cfg.Backend)
	}
}
