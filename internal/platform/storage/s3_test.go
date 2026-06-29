package storage

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// fakeS3 is an interface-level fake of the S3 API: it records the last
// Put/Delete call so the driver's key/body/content-type wiring can be asserted
// WITHOUT a live bucket (the local backend remains the tested default path; S3
// behavior here is covered at the interface seam, with live S3 env-guarded).
type fakeS3 struct {
	putBucket, putKey, putContentType string
	putBody                           string
	delBucket, delKey                 string
	putErr, delErr                    error
}

func (f *fakeS3) PutObject(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if f.putErr != nil {
		return nil, f.putErr
	}
	f.putBucket = *in.Bucket
	f.putKey = *in.Key
	if in.ContentType != nil {
		f.putContentType = *in.ContentType
	}
	if in.Body != nil {
		b, _ := io.ReadAll(in.Body)
		f.putBody = string(b)
	}
	return &s3.PutObjectOutput{}, nil
}

func (f *fakeS3) DeleteObject(_ context.Context, in *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if f.delErr != nil {
		return nil, f.delErr
	}
	f.delBucket = *in.Bucket
	f.delKey = *in.Key
	return &s3.DeleteObjectOutput{}, nil
}

func newFakeDriver(cfg S3Config) (*S3Storage, *fakeS3) {
	fake := &fakeS3{}
	return newS3StorageWithClient(fake, cfg), fake
}

func TestS3Save_PutsObjectWithKeyAndContentType(t *testing.T) {
	drv, fake := newFakeDriver(S3Config{Bucket: "media", Region: "us-east-1"})
	key, err := drv.Save(context.Background(), "media/2026/abc.png", strings.NewReader("PNGDATA"), "image/png")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if key != "media/2026/abc.png" {
		t.Errorf("returned key = %q", key)
	}
	if fake.putBucket != "media" || fake.putKey != "media/2026/abc.png" {
		t.Errorf("put target = %s/%s", fake.putBucket, fake.putKey)
	}
	if fake.putContentType != "image/png" {
		t.Errorf("content type = %q", fake.putContentType)
	}
	if fake.putBody != "PNGDATA" {
		t.Errorf("body = %q", fake.putBody)
	}
}

func TestS3Save_TrimsLeadingSlash(t *testing.T) {
	drv, fake := newFakeDriver(S3Config{Bucket: "b"})
	if _, err := drv.Save(context.Background(), "/leading/slash.jpg", strings.NewReader("x"), ""); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if fake.putKey != "leading/slash.jpg" {
		t.Errorf("key not normalized: %q", fake.putKey)
	}
}

func TestS3Delete_DeletesObject(t *testing.T) {
	drv, fake := newFakeDriver(S3Config{Bucket: "b"})
	if err := drv.Delete(context.Background(), "k/obj.png"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if fake.delBucket != "b" || fake.delKey != "k/obj.png" {
		t.Errorf("delete target = %s/%s", fake.delBucket, fake.delKey)
	}
}

func TestS3Delete_EmptyKeyIsNoop(t *testing.T) {
	drv, fake := newFakeDriver(S3Config{Bucket: "b"})
	if err := drv.Delete(context.Background(), ""); err != nil {
		t.Fatalf("Delete empty: %v", err)
	}
	if fake.delKey != "" {
		t.Error("empty key should not reach the API")
	}
}

func TestS3Save_PropagatesError(t *testing.T) {
	fake := &fakeS3{putErr: errors.New("boom")}
	drv := newS3StorageWithClient(fake, S3Config{Bucket: "b"})
	if _, err := drv.Save(context.Background(), "k.png", strings.NewReader("x"), ""); err == nil {
		t.Fatal("expected put error to surface")
	}
}

func TestS3URL_AWSVirtualHosted(t *testing.T) {
	drv := newS3StorageWithClient(&fakeS3{}, S3Config{Bucket: "media", Region: "eu-central-1"})
	got := drv.URL("path/to/obj.png")
	want := "https://media.s3.eu-central-1.amazonaws.com/path/to/obj.png"
	if got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
}

func TestS3URL_PublicBaseURLWins(t *testing.T) {
	drv := newS3StorageWithClient(&fakeS3{}, S3Config{
		Bucket: "media", Region: "us-east-1", PublicBaseURL: "https://cdn.example.com/assets/",
	})
	if got := drv.URL("a/b.png"); got != "https://cdn.example.com/assets/a/b.png" {
		t.Errorf("URL = %q", got)
	}
}

func TestS3URL_PathStyleEndpoint(t *testing.T) {
	// MinIO-style: custom endpoint + path-style.
	drv := newS3StorageWithClient(&fakeS3{}, S3Config{
		Bucket: "media", Endpoint: "http://localhost:9000", UsePathStyle: true,
	})
	if got := drv.URL("x/y.png"); got != "http://localhost:9000/media/x/y.png" {
		t.Errorf("URL = %q", got)
	}
}

func TestS3URL_VirtualHostEndpoint(t *testing.T) {
	drv := newS3StorageWithClient(&fakeS3{}, S3Config{
		Bucket: "media", Endpoint: "https://r2.example.com", UsePathStyle: false,
	})
	if got := drv.URL("x/y.png"); got != "https://media.r2.example.com/x/y.png" {
		t.Errorf("URL = %q", got)
	}
}

func TestS3URL_EmptyKey(t *testing.T) {
	drv := newS3StorageWithClient(&fakeS3{}, S3Config{Bucket: "b"})
	if got := drv.URL(""); got != "" {
		t.Errorf("empty key URL = %q, want empty", got)
	}
}

func TestNewS3Storage_RequiresBucket(t *testing.T) {
	if _, err := NewS3Storage(context.Background(), S3Config{Region: "us-east-1"}); err == nil {
		t.Fatal("expected error when bucket is empty")
	}
}
