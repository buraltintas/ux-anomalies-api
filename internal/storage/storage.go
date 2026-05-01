package storage

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"cloud.google.com/go/storage"
)

type UploadResult struct {
	Bucket     string
	ObjectName string
	MimeType   string
	ByteSize   int64
}

type Store interface {
	UploadImage(ctx context.Context, objectName, mimeType string, data []byte) (UploadResult, error)
	SignedGetURL(ctx context.Context, bucket, objectName string, ttl time.Duration) (string, error)
}

type GCSStore struct {
	client *storage.Client
	bucket string
}

func NewGCSStore(ctx context.Context, bucket string) (*GCSStore, error) {
	c, err := storage.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return &GCSStore{client: c, bucket: bucket}, nil
}

func (s *GCSStore) UploadImage(ctx context.Context, objectName, mimeType string, data []byte) (UploadResult, error) {
	w := s.client.Bucket(s.bucket).Object(objectName).NewWriter(ctx)
	w.ContentType = mimeType
	if _, err := bytes.NewReader(data).WriteTo(w); err != nil {
		_ = w.Close()
		return UploadResult{}, err
	}
	if err := w.Close(); err != nil {
		return UploadResult{}, err
	}
	return UploadResult{
		Bucket:     s.bucket,
		ObjectName: objectName,
		MimeType:   mimeType,
		ByteSize:   int64(len(data)),
	}, nil
}

func (s *GCSStore) SignedGetURL(_ context.Context, bucket, objectName string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	u, err := s.client.Bucket(bucket).SignedURL(objectName, &storage.SignedURLOptions{
		Method:  http.MethodGet,
		Expires: time.Now().Add(ttl),
		Scheme:  storage.SigningSchemeV4,
	})
	if err != nil {
		return "", err
	}
	return u, nil
}

func (s *GCSStore) Close() error { return s.client.Close() }

type LocalStore struct {
	baseDir string
}

func NewLocalStore(baseDir string) *LocalStore { return &LocalStore{baseDir: baseDir} }

func (l *LocalStore) UploadImage(_ context.Context, objectName, mimeType string, data []byte) (UploadResult, error) {
	path := filepath.Join(l.baseDir, objectName)
	if err := writeFile(path, data); err != nil {
		return UploadResult{}, err
	}
	return UploadResult{Bucket: "local", ObjectName: objectName, MimeType: mimeType, ByteSize: int64(len(data))}, nil
}

func (l *LocalStore) SignedGetURL(_ context.Context, _ string, objectName string, _ time.Duration) (string, error) {
	return fmt.Sprintf("/local/%s", objectName), nil
}
