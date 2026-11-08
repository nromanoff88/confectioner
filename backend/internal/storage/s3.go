package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"confectioner-backend/internal/config"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type S3Storage struct {
	client    *minio.Client
	bucket    string
	publicURL string
}

func NewS3Storage(ctx context.Context, cfg *config.Config) (*S3Storage, error) {
	mc, err := minio.New(cfg.S3Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.S3AccessKey, cfg.S3SecretKey, ""),
		Secure: cfg.S3UseSSL,
		Region: cfg.S3Region,
	})
	if err != nil {
		return nil, fmt.Errorf("init minio client: %w", err)
	}

	exists, err := mc.BucketExists(ctx, cfg.S3Bucket)
	if err != nil {
		return nil, fmt.Errorf("check bucket %s: %w", cfg.S3Bucket, err)
	}
	if !exists {
		if err := mc.MakeBucket(ctx, cfg.S3Bucket, minio.MakeBucketOptions{Region: cfg.S3Region}); err != nil {
			return nil, fmt.Errorf("create bucket %s: %w", cfg.S3Bucket, err)
		}
	}

	return &S3Storage{
		client:    mc,
		bucket:    cfg.S3Bucket,
		publicURL: cfg.S3PublicURL,
	}, nil
}

func (s *S3Storage) MediaURL(s3Key string) string {
	if s.publicURL != "" {
		return s.publicURL + "/" + strings.TrimLeft(s3Key, "/")
	}
	return "/api/media/" + strings.TrimLeft(s3Key, "/")
}

func (s *S3Storage) Upload(ctx context.Context, folder, originalName, contentType string, r io.Reader, size int64) (string, string, error) {
	ext := filepath.Ext(originalName)
	if ext == "" {
		switch contentType {
		case "image/png":
			ext = ".png"
		case "image/webp":
			ext = ".webp"
		case "image/svg+xml":
			ext = ".svg"
		default:
			ext = ".jpg"
		}
	}
	if contentType == "" {
		contentType = "image/jpeg"
	}

	var rnd [8]byte
	_, _ = rand.Read(rnd[:])
	key := fmt.Sprintf("%s/%s_%s%s", strings.Trim(folder, "/"), time.Now().Format("20060102_150405"), hex.EncodeToString(rnd[:]), ext)

	_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return "", "", fmt.Errorf("put object %s: %w", key, err)
	}

	return key, s.MediaURL(key), nil
}

func (s *S3Storage) UploadFromURL(ctx context.Context, folder, remoteURL string) (string, string, []byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, remoteURL, nil)
	if err != nil {
		return "", "", nil, "", err
	}
	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", nil, "", fmt.Errorf("download from %s: %w", remoteURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", nil, "", fmt.Errorf("download status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 25<<20)) // max 25 MB
	if err != nil {
		return "", "", nil, "", err
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}

	key, url, err := s.Upload(ctx, folder, "photo.jpg", contentType, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", "", nil, "", err
	}
	return key, url, data, contentType, nil
}

func (s *S3Storage) PutStaticKey(ctx context.Context, key, contentType string, data []byte) (string, error) {
	_, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err == nil {
		return s.MediaURL(key), nil
	}
	_, err = s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return "", err
	}
	return s.MediaURL(key), nil
}

func (s *S3Storage) GetObject(ctx context.Context, key string) (*minio.Object, minio.ObjectInfo, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, minio.ObjectInfo{}, err
	}
	info, err := obj.Stat()
	if err != nil {
		_ = obj.Close()
		return nil, minio.ObjectInfo{}, err
	}
	return obj, info, nil
}

func (s *S3Storage) GetObjectBytes(ctx context.Context, key string) ([]byte, string, error) {
	obj, info, err := s.GetObject(ctx, key)
	if err != nil {
		return nil, "", err
	}
	defer obj.Close()

	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, "", err
	}
	return data, info.ContentType, nil
}

