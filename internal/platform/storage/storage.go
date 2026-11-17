// Package storage wraps S3-compatible object storage (Neon storage).
package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Config holds the connection settings. All fields are required.
type Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
}

// Enabled reports whether storage is fully configured.
func (c Config) Enabled() bool {
	return c.Endpoint != "" && c.Region != "" && c.Bucket != "" && c.AccessKeyID != "" && c.SecretAccessKey != ""
}

// Client reads and writes objects in one bucket.
type Client struct {
	s3      *s3.Client
	presign *s3.PresignClient
	bucket  string
}

// New creates a Client. Checksums are only sent when required because many
// S3-compatible services do not accept the SDK's default trailing checksums.
func New(cfg Config) (*Client, error) {
	if !cfg.Enabled() {
		return nil, errors.New("storage: incomplete configuration")
	}
	c := s3.New(s3.Options{
		Region:                     cfg.Region,
		BaseEndpoint:               aws.String(cfg.Endpoint),
		Credentials:                credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		UsePathStyle:               true,
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	})
	return &Client{s3: c, presign: s3.NewPresignClient(c), bucket: cfg.Bucket}, nil
}

// Put stores data under key.
func (c *Client) Put(ctx context.Context, key, contentType string, data []byte) error {
	_, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(data),
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(int64(len(data))),
	})
	if err != nil {
		return fmt.Errorf("storage put %q: %w", key, err)
	}
	return nil
}

// Delete removes an object. Deleting a missing object is not an error.
func (c *Client) Delete(ctx context.Context, key string) error {
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)})
	if err != nil {
		return fmt.Errorf("storage delete %q: %w", key, err)
	}
	return nil
}

// PresignGet returns a temporary URL that lets a client download the object.
func (c *Client) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	req, err := c.presign.PresignGetObject(ctx,
		&s3.GetObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)},
		s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("storage presign %q: %w", key, err)
	}
	return req.URL, nil
}
