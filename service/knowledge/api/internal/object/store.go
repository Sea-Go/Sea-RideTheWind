// Package object owns immutable content addressed objects. Backend refs are keys, not URLs.
package object

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/minio/minio-go/v7"
)

const MaxObjectBytes = 16 << 20

type Store interface {
	Put(context.Context, []byte) (string, string, error)
	Get(context.Context, string, string) ([]byte, error)
}

func Hash(b []byte) string   { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func Key(hash string) string { return "sha256/" + hash }
func validKey(key, hash string) bool {
	b, err := hex.DecodeString(hash)
	return err == nil && len(b) == sha256.Size && strings.ToLower(hash) == hash && key == Key(hash)
}
func verify(data []byte, hash string) ([]byte, error) {
	if len(data) > MaxObjectBytes || Hash(data) != hash {
		return nil, fmt.Errorf("object hash or size mismatch")
	}
	return data, nil
}

type Local struct{ root string }

func NewLocal(root string) (*Local, error) {
	if root == "" {
		return nil, errors.New("local object directory required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Join(abs, "sha256"), 0700); err != nil {
		return nil, err
	}
	return &Local{root: abs}, nil
}
func (s *Local) Put(ctx context.Context, data []byte) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if len(data) > MaxObjectBytes {
		return "", "", errors.New("object too large")
	}
	hash := Hash(data)
	key := Key(hash)
	dest := filepath.Join(s.root, key)
	tmp, err := os.CreateTemp(filepath.Join(s.root, "sha256"), ".staging-")
	if err != nil {
		return "", "", err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return "", "", err
	}
	if err = ctx.Err(); err != nil {
		return "", "", err
	}
	// Link is atomic and never replaces an existing immutable object.
	if err = os.Link(tmp.Name(), dest); err != nil && !errors.Is(err, os.ErrExist) {
		return "", "", err
	}
	if _, err = s.Get(ctx, key, hash); err != nil {
		return "", "", err
	}
	return key, hash, nil
}
func (s *Local) Get(ctx context.Context, key, hash string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validKey(key, hash) {
		return nil, errors.New("invalid object reference")
	}
	f, err := os.Open(filepath.Join(s.root, key))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxObjectBytes+1))
	if err != nil {
		return nil, err
	}
	return verify(b, hash)
}

type S3 struct {
	client *minio.Client
	bucket string
}

func NewS3(client *minio.Client, bucket string) *S3 { return &S3{client: client, bucket: bucket} }
func (s *S3) Put(ctx context.Context, data []byte) (string, string, error) {
	if len(data) > MaxObjectBytes {
		return "", "", errors.New("object too large")
	}
	hash := Hash(data)
	key := Key(hash)
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{ContentType: "application/octet-stream"})
	if err != nil {
		return "", "", err
	}
	if _, err = s.Get(ctx, key, hash); err != nil {
		return "", "", err
	}
	return key, hash, nil
}
func (s *S3) Get(ctx context.Context, key, hash string) ([]byte, error) {
	if !validKey(key, hash) {
		return nil, errors.New("invalid object reference")
	}
	f, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxObjectBytes+1))
	if err != nil {
		return nil, err
	}
	return verify(b, hash)
}
