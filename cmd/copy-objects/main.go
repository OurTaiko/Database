// copy-objects copies every file the database refers to from a local object
// directory into the configured S3 store. Each file is checked against its
// recorded SHA-256 and size first. Objects already in S3 with the right size
// are skipped, and S3 refuses to overwrite a key, so a rerun is safe.
// Without -apply it only checks the local files and counts what S3 lacks.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/joho/godotenv"
	"ourtaiko.dev/database/api/internal/audio"
	"ourtaiko.dev/database/api/internal/database"
	"ourtaiko.dev/database/api/internal/objectstore"
)

type record struct {
	key, sha, extension string
	size                int64
}

func media(extension string) string {
	switch extension {
	case "webp":
		return "image/webp"
	case "ogg", "mp3":
		return audio.MediaType("x." + extension)
	default:
		return "application/octet-stream"
	}
}

func check(path string, r record) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return err
	}
	if n != r.size || hex.EncodeToString(h.Sum(nil)) != r.sha {
		return fmt.Errorf("%s does not match its database record", r.key)
	}
	return nil
}

func main() {
	envFiles := flag.String("env", ".env", "env file with DATABASE_URL and the destination S3 settings")
	source := flag.String("source", ".data/files", "local object directory")
	apply := flag.Bool("apply", false, "upload the missing objects")
	workers := flag.Int("workers", 8, "parallel uploads")
	flag.Parse()
	if err := godotenv.Load(*envFiles); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatal("cannot parse ", *envFiles)
	}
	if os.Getenv("STORAGE_BACKEND") != "s3" {
		log.Fatal("set STORAGE_BACKEND=s3 and the S3 settings for the destination")
	}
	ctx := context.Background()
	objects, err := objectstore.FromEnv(*source)
	if err != nil {
		log.Fatal(err)
	}
	pool, err := database.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	rows, err := pool.Query(ctx, `SELECT storage_key,sha256,extension,byte_size FROM chart_resources ORDER BY id`)
	if err != nil {
		log.Fatal(err)
	}
	var records []record
	for rows.Next() {
		var r record
		if err = rows.Scan(&r.key, &r.sha, &r.extension, &r.size); err != nil {
			log.Fatal(err)
		}
		records = append(records, r)
	}
	if err = rows.Err(); err != nil {
		log.Fatal(err)
	}
	var present, missing, uploaded, bytes atomic.Int64
	var failure error
	var once sync.Once
	jobs := make(chan record)
	var wg sync.WaitGroup
	for range max(1, *workers) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range jobs {
				fail := func(err error) { once.Do(func() { failure = err }) }
				path := filepath.Join(*source, r.key)
				if err := check(path, r); err != nil {
					fail(err)
					continue
				}
				if existing, err := objects.Open(ctx, r.key); err == nil {
					size := existing.Size()
					existing.Close()
					if size != r.size {
						fail(fmt.Errorf("%s exists in S3 with a different size", r.key))
						continue
					}
					present.Add(1)
					continue
				} else if !errors.Is(err, os.ErrNotExist) {
					fail(err)
					continue
				}
				missing.Add(1)
				if !*apply {
					continue
				}
				f, err := os.Open(path)
				if err != nil {
					fail(err)
					continue
				}
				err = objects.Put(ctx, r.key, f, r.size, media(r.extension), r.sha)
				f.Close()
				if err != nil {
					fail(fmt.Errorf("%s: %w", r.key, err))
					continue
				}
				if n := uploaded.Add(1); n%200 == 0 {
					log.Printf("uploaded %d objects", n)
				}
				bytes.Add(r.size)
			}
		}()
	}
	for _, r := range records {
		jobs <- r
	}
	close(jobs)
	wg.Wait()
	log.Printf("%d records: %d already in S3, %d missing, %d uploaded (%.1f MiB)", len(records), present.Load(), missing.Load(), uploaded.Load(), float64(bytes.Load())/(1<<20))
	if failure != nil {
		log.Fatal(failure)
	}
}
