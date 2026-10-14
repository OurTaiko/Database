// import-ese loads the ESE chart repository into the official chart database.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/joho/godotenv"
	"ourtaiko.dev/database/api/internal/database"
	"ourtaiko.dev/database/api/internal/ese"
	"ourtaiko.dev/database/api/internal/objectstore"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatal("Cannot parse .env; check its syntax")
	}
	root := flag.String("ese", env("ESE_ROOT", ""), "path of the ESE repository checkout")
	mapping := flag.String("mapping", env("ESE_MAPPING", ""), "path of the Nijiiro ID mapping data.json")
	dryRun := flag.Bool("dry-run", false, "parse and hash only; write no files or rows")
	reportPath := flag.String("report", "", "write the JSON report to this file")
	flag.Parse()
	if *root == "" || *mapping == "" {
		log.Fatal("-ese and -mapping (or ESE_ROOT and ESE_MAPPING) are required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := database.Open(ctx, env("DATABASE_URL", "postgres://localhost/ourtaiko_database?host=/tmp&sslmode=disable"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	if !*dryRun {
		if err = database.Migrate(ctx, pool); err != nil {
			log.Fatal(err)
		}
	}
	objects, err := objectstore.FromEnv(env("STORAGE_DIR", ".data/files"))
	if err != nil {
		log.Fatal(err)
	}
	report, err := ese.Run(ctx, pool, ese.Options{Root: *root, Mapping: *mapping, Objects: objects, Workers: runtime.NumCPU(), DryRun: *dryRun})
	if err != nil {
		log.Fatal(err)
	}
	out, _ := json.MarshalIndent(report, "", "  ")
	if *reportPath != "" {
		if err = os.WriteFile(*reportPath, append(out, '\n'), 0644); err != nil {
			log.Fatal(err)
		}
	}
	log.Printf("scanned %d, added %d, updated %d, unchanged %d, skipped (deleted on site) %d, excluded %d; %d with a Nijiiro ID; %d mapping entries without a chart",
		report.Scanned, report.Added, report.Updated, report.Unchanged, report.Removed, report.Excluded, report.Mapped, len(report.Unmatched))
	for path, issue := range report.Issues {
		log.Printf("excluded %s: %s", path, issue)
	}
}
