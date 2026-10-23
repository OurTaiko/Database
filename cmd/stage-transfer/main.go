// stage-transfer uploads or deletes temporary transfer files (image tars for
// a server that cannot reach Docker Hub) under the database/ prefix. The
// server then fetches them through the CDN. Use a random directory name and
// delete the files as soon as they are loaded.
// Credentials come from the given env file (see .env.s3).
//
//	stage-transfer -env FILE put DIR FILE...    prints "sha256 key" per file
//	stage-transfer -env FILE delete DIR NAME...
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/joho/godotenv"
)

func main() {
	env := flag.String("env", "", "env file")
	flag.Parse()
	if err := godotenv.Load(*env); err != nil {
		log.Fatal(err)
	}
	args := flag.Args()
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(os.Getenv("AWS_REGION")))
	if err != nil {
		log.Fatal(err)
	}
	client := s3.NewFromConfig(cfg)
	bucket, prefix := os.Getenv("S3_BUCKET"), os.Getenv("S3_PREFIX")+"/"+args[1]+"/"
	switch args[0] {
	case "put":
		up := manager.NewUploader(client, func(u *manager.Uploader) { u.PartSize = 32 << 20; u.Concurrency = 6 })
		for _, name := range args[2:] {
			f, err := os.Open(name)
			if err != nil {
				log.Fatal(err)
			}
			h := sha256.New()
			io.Copy(h, f)
			f.Seek(0, 0)
			key := prefix + filepath.Base(name)
			_, err = up.Upload(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), Body: f, ContentType: aws.String("application/x-tar"), CacheControl: aws.String("no-store")})
			f.Close()
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println(hex.EncodeToString(h.Sum(nil)), key)
		}
	case "delete":
		for _, name := range args[2:] {
			if _, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(prefix + name)}); err != nil {
				log.Fatal(err)
			}
			fmt.Println("deleted", prefix+name)
		}
	}
}
