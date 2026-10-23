package objectstore

import (
	"context"
	"errors"
	"os"
	"time"
)

// FromEnv builds the permanent store the environment selects:
// STORAGE_BACKEND=local (default) keeps objects under localRoot;
// STORAGE_BACKEND=s3 uses S3_BUCKET, AWS_REGION, S3_PREFIX and, for public
// links, S3_PUBLIC_BASE_URL (the CDN address of S3_PREFIX). Credentials come
// from the AWS SDK chain.
func FromEnv(localRoot string) (Store, error) {
	switch os.Getenv("STORAGE_BACKEND") {
	case "", "local":
		return Local{Root: localRoot}, nil
	case "s3":
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store, err := NewS3(ctx, os.Getenv("S3_BUCKET"), os.Getenv("AWS_REGION"), os.Getenv("S3_PREFIX"))
		if err != nil {
			return nil, err
		}
		return store, store.SetPublicBaseURL(os.Getenv("S3_PUBLIC_BASE_URL"))
	default:
		return nil, errors.New("STORAGE_BACKEND must be local or s3")
	}
}
