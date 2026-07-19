package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/s3utils"
)

type s3Store struct {
	client *minio.Client
	bucket string
	prefix string
	lookup minio.BucketLookupType
}

func newS3Store(ctx context.Context, cfg Config) (Store, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	bucket := strings.TrimSpace(cfg.Bucket)
	accessKeyID := strings.TrimSpace(cfg.AccessKeyID)
	secretAccessKey := strings.TrimSpace(cfg.SecretAccessKey)
	sessionToken := strings.TrimSpace(cfg.SessionToken)

	switch {
	case endpoint == "":
		return nil, errors.New("s3 object store endpoint is required")
	case bucket == "":
		return nil, errors.New("s3 object store bucket is required")
	case sessionToken != "" && (accessKeyID == "" || secretAccessKey == ""):
		return nil, errors.New("s3 object store session token requires static credentials")
	case accessKeyID == "" || secretAccessKey == "":
		return nil, errors.New("s3 object store access key id and secret access key are required")
	}
	if err := s3utils.CheckValidBucketNameStrict(bucket); err != nil {
		return nil, fmt.Errorf("s3 object store bucket is invalid: %w", err)
	}
	if err := contextError(ctx); err != nil {
		return nil, fmt.Errorf("initialize s3 object store: %w", err)
	}

	lookup := minio.BucketLookupAuto
	if cfg.PathStyle {
		lookup = minio.BucketLookupPath
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(accessKeyID, secretAccessKey, sessionToken),
		Secure:       cfg.Secure,
		Region:       strings.TrimSpace(cfg.Region),
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, fmt.Errorf("create s3 object store client: %w", err)
	}
	store := &s3Store{client: client, bucket: bucket, prefix: cfg.Prefix, lookup: lookup}
	if cfg.AutoCreateBucket {
		if err := store.ensureBucket(ctx, strings.TrimSpace(cfg.Region)); err != nil {
			return nil, err
		}
	}
	return store, nil
}

func (s *s3Store) Put(ctx context.Context, key string, src io.Reader, size int64, contentType string) (Info, error) {
	key, err := normalizeKey(key)
	if err != nil {
		return Info{}, err
	}
	if src == nil {
		return Info{}, errors.New("put s3 object: source is required")
	}
	if size < -1 {
		return Info{}, errors.New("put s3 object: size must be -1 or greater")
	}
	if err := contextError(ctx); err != nil {
		return Info{}, fmt.Errorf("put s3 object %q: %w", key, err)
	}
	contentType = normalizedContentType(contentType)
	upload, err := s.client.PutObject(
		ctx,
		s.bucket,
		prefixedKey(s.prefix, key),
		contextReader{ctx: ctx, src: src},
		size,
		minio.PutObjectOptions{ContentType: contentType},
	)
	if err != nil {
		return Info{}, fmt.Errorf("put s3 object %q: %w", key, mapS3Error(err))
	}
	return Info{
		Key: key, Size: upload.Size, ContentType: contentType,
		ETag: upload.ETag, LastModified: upload.LastModified.UTC(),
	}, nil
}

func (s *s3Store) Open(ctx context.Context, key string) (Reader, error) {
	key, err := normalizeKey(key)
	if err != nil {
		return nil, err
	}
	if err := contextError(ctx); err != nil {
		return nil, fmt.Errorf("open s3 object %q: %w", key, err)
	}
	object, err := s.client.GetObject(ctx, s.bucket, prefixedKey(s.prefix, key), minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("open s3 object %q: %w", key, mapS3Error(err))
	}
	if _, err := object.Stat(); err != nil {
		_ = object.Close()
		return nil, fmt.Errorf("open s3 object %q: %w", key, mapS3Error(err))
	}
	return object, nil
}

func (s *s3Store) Stat(ctx context.Context, key string) (Info, error) {
	key, err := normalizeKey(key)
	if err != nil {
		return Info{}, err
	}
	if err := contextError(ctx); err != nil {
		return Info{}, fmt.Errorf("stat s3 object %q: %w", key, err)
	}
	object, err := s.client.StatObject(ctx, s.bucket, prefixedKey(s.prefix, key), minio.StatObjectOptions{})
	if err != nil {
		return Info{}, fmt.Errorf("stat s3 object %q: %w", key, mapS3Error(err))
	}
	return Info{
		Key: key, Size: object.Size, ContentType: normalizedContentType(object.ContentType),
		ETag: object.ETag, LastModified: object.LastModified.UTC(),
	}, nil
}

func (s *s3Store) Delete(ctx context.Context, key string) error {
	key, err := normalizeKey(key)
	if err != nil {
		return err
	}
	if err := contextError(ctx); err != nil {
		return fmt.Errorf("delete s3 object %q: %w", key, err)
	}
	if err := s.client.RemoveObject(ctx, s.bucket, prefixedKey(s.prefix, key), minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete s3 object %q: %w", key, mapS3Error(err))
	}
	return nil
}

func (s *s3Store) ensureBucket(ctx context.Context, region string) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("check s3 object store bucket %q: %w", s.bucket, err)
	}
	if exists {
		return nil
	}
	if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{Region: region}); err != nil {
		return fmt.Errorf("create s3 object store bucket %q: %w", s.bucket, err)
	}
	return nil
}

func mapS3Error(err error) error {
	response := minio.ToErrorResponse(err)
	if response.StatusCode == http.StatusNotFound ||
		response.Code == "NoSuchKey" ||
		response.Code == "NoSuchObject" {
		return errors.Join(ErrNotFound, err)
	}
	return err
}
