package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// s3API is the narrow subset of the AWS S3 client the driver uses. Declaring it
// here (rather than depending on *s3.Client directly) lets the driver be unit
// tested against an interface-level fake WITHOUT a live bucket — the key/URL
// logic and the Save/Delete call shapes are all exercised against the fake. The
// real *s3.Client satisfies it.
type s3API interface {
	PutObject(ctx context.Context, in *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObject(ctx context.Context, in *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

// S3Config configures the S3 (or S3-compatible: MinIO, Cloudflare R2, etc.)
// storage driver. Endpoint/PathStyle support non-AWS providers; AccessKey/Secret
// are optional (empty falls back to the AWS default credential chain — env,
// shared config, IAM role). PublicBaseURL, when set, is the CDN/website base
// used to build object URLs; otherwise URLs are derived from the endpoint/bucket.
type S3Config struct {
	Bucket          string
	Region          string
	Endpoint        string // custom endpoint for S3-compatible providers; empty = AWS
	AccessKeyID     string
	SecretAccessKey string
	UsePathStyle    bool   // true for MinIO/most S3-compatible providers
	PublicBaseURL   string // optional CDN/website base for URL(); no trailing slash needed
}

// S3Storage stores objects in an S3 (or S3-compatible) bucket behind the same
// Storage interface as LocalStorage, so the rest of the app is backend-agnostic.
type S3Storage struct {
	client        s3API
	bucket        string
	endpoint      string
	region        string
	usePathStyle  bool
	publicBaseURL string
}

var _ Storage = (*S3Storage)(nil)

// NewS3Storage constructs the driver from config, building the AWS S3 client.
// Static credentials are used when both are present; otherwise the default
// credential chain is used. A custom endpoint (MinIO/R2) is applied via
// BaseEndpoint. The bucket is required.
func NewS3Storage(ctx context.Context, cfg S3Config) (*S3Storage, error) {
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, errors.New("storage: s3 bucket is required")
	}

	loadOpts := []func(*awsconfig.LoadOptions) error{}
	if cfg.Region != "" {
		loadOpts = append(loadOpts, awsconfig.WithRegion(cfg.Region))
	}
	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("storage: load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = cfg.UsePathStyle
	})

	return newS3StorageWithClient(client, cfg), nil
}

// newS3StorageWithClient builds the driver around a provided s3API — the seam
// the unit tests use to inject a fake (no network, no live bucket).
func newS3StorageWithClient(client s3API, cfg S3Config) *S3Storage {
	return &S3Storage{
		client:        client,
		bucket:        cfg.Bucket,
		endpoint:      strings.TrimRight(cfg.Endpoint, "/"),
		region:        cfg.Region,
		usePathStyle:  cfg.UsePathStyle,
		publicBaseURL: strings.TrimRight(cfg.PublicBaseURL, "/"),
	}
}

// Save streams r to s3://bucket/key with the given content type and returns key.
// The caller-derived key is already sanitized (ObjectKey), so it is used as-is.
func (s *S3Storage) Save(ctx context.Context, key string, r io.Reader, contentType string) (string, error) {
	key = strings.TrimLeft(key, "/")
	in := &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   r,
	}
	if contentType != "" {
		in.ContentType = aws.String(contentType)
	}
	if _, err := s.client.PutObject(ctx, in); err != nil {
		return "", fmt.Errorf("storage: s3 put %q: %w", key, err)
	}
	return key, nil
}

// Delete removes the object at key. S3 DeleteObject is idempotent (deleting a
// missing key succeeds), matching the LocalStorage contract.
func (s *S3Storage) Delete(ctx context.Context, key string) error {
	if key == "" {
		return nil
	}
	key = strings.TrimLeft(key, "/")
	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}); err != nil {
		return fmt.Errorf("storage: s3 delete %q: %w", key, err)
	}
	return nil
}

// URL returns the public URL for key. Resolution order: an explicit
// PublicBaseURL (CDN/website) wins; else a custom endpoint yields
// path-style (endpoint/bucket/key) or virtual-host (bucket.endpoint-host/key);
// else the canonical AWS virtual-hosted URL. The key is the object path.
func (s *S3Storage) URL(key string) string {
	if key == "" {
		return ""
	}
	key = strings.TrimLeft(key, "/")

	if s.publicBaseURL != "" {
		return s.publicBaseURL + "/" + key
	}

	if s.endpoint != "" {
		if s.usePathStyle {
			return s.endpoint + "/" + s.bucket + "/" + key
		}
		// Virtual-host style against a custom endpoint: prepend the bucket to the
		// endpoint host (scheme://bucket.host/key).
		if scheme, host, ok := splitScheme(s.endpoint); ok {
			return scheme + "://" + s.bucket + "." + host + "/" + key
		}
		return s.endpoint + "/" + s.bucket + "/" + key
	}

	region := s.region
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", s.bucket, region, key)
}

// splitScheme splits "scheme://host[/...]" into scheme and host (host only, no
// path). It returns ok=false when there is no "://".
func splitScheme(raw string) (scheme, host string, ok bool) {
	i := strings.Index(raw, "://")
	if i < 0 {
		return "", "", false
	}
	scheme = raw[:i]
	rest := raw[i+3:]
	if j := strings.IndexByte(rest, '/'); j >= 0 {
		rest = rest[:j]
	}
	return scheme, rest, true
}
