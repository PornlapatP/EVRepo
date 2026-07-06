package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// DefaultPresignTTL is how long a presigned read URL stays valid.
const DefaultPresignTTL = 15 * time.Minute

// Service uploads files to S3 (or an S3-compatible endpoint). This storage
// backend (PEA's Dell EMC ECS, at geodrive.pea.co.th) has no anonymous public
// read — every read needs a signed request — so there is no permanent
// "public URL" to hand back from Upload; use PresignGet to read a file back.
type Service struct {
	client        *s3.Client
	presignClient *s3.PresignClient
	bucket        string
}

// New builds a Service from env vars:
//   - AWS_REGION, AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY (picked up automatically
//     by the SDK's default credential chain — in production prefer an IAM role
//     attached to the compute instance instead of static keys, no code change needed)
//   - S3_BUCKET_NAME — the bucket to write to
//   - AWS_ENDPOINT_URL — the S3-compatible endpoint (e.g. https://geodrive.pea.co.th:9021)
func New(ctx context.Context) (*Service, error) {
	bucket := os.Getenv("S3_BUCKET_NAME")
	if bucket == "" {
		return nil, fmt.Errorf("S3_BUCKET_NAME is required")
	}

	optFns := []func(*config.LoadOptions) error{}
	if endpoint := os.Getenv("AWS_ENDPOINT_URL"); endpoint != "" {
		optFns = append(optFns, config.WithBaseEndpoint(endpoint))
	}

	cfg, err := config.LoadDefaultConfig(ctx, optFns...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if os.Getenv("AWS_ENDPOINT_URL") != "" {
			o.UsePathStyle = true // required for MinIO/Dell EMC ECS/S3-compatible endpoints
		}
		// Third-party S3-compatible services often don't support the SDK's
		// newer default of sending flexible checksums over aws-chunked
		// trailers — that mismatch is what causes XAmzContentSHA256Mismatch
		// against them. Only compute/send a checksum when an operation
		// actually requires one (reverts to a plain upfront SHA256 body hash).
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})

	return &Service{
		client:        client,
		presignClient: s3.NewPresignClient(client),
		bucket:        bucket,
	}, nil
}

// Upload puts data at key in the bucket and returns the key unchanged (there
// is no public URL to construct — see PresignGet to read it back later).
func (s *Service) Upload(ctx context.Context, key string, data io.Reader, contentType string) (string, error) {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        data,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("s3 upload %q: %w", key, err)
	}

	return key, nil
}

// PresignGet returns a short-lived signed URL for reading back an uploaded
// object. This is a local signature computation, not a network call.
func (s *Service) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	req, err := s.presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("presign %q: %w", key, err)
	}

	return req.URL, nil
}
