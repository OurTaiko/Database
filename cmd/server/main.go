package main

import (
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	webapp "ourtaiko.dev/database/api"
	"ourtaiko.dev/database/api/internal/charthash"
	"ourtaiko.dev/database/api/internal/database"
	"ourtaiko.dev/database/api/internal/httpapi"
	"ourtaiko.dev/database/api/internal/tja"
	"ourtaiko.dev/sitekit/objectstore"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func loopback(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}

func validUserID(id string) bool {
	b, err := hex.DecodeString(id)
	return err == nil && len(b) == 16 && strings.ToLower(id) == id
}

func main() {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatal("Cannot parse .env; check its syntax")
	}
	migrateOnly := flag.Bool("migrate", false, "apply migrations and exit")
	grant := flag.String("grant-upload", "", "give this user ID the upload permission and exit")
	revoke := flag.String("revoke-upload", "", "take the upload permission from this user ID and exit")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	pool, err := database.Open(ctx, env("DATABASE_URL", "postgres://localhost/ourtaiko_database?host=/tmp&sslmode=disable"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	if err = database.Migrate(ctx, pool); err != nil {
		log.Fatal(err)
	}
	if *migrateOnly {
		log.Print("PostgreSQL migrations completed")
		return
	}
	if id := *grant + *revoke; id != "" {
		if !validUserID(id) || (*grant != "" && *revoke != "") {
			log.Fatal("give one 32-character lowercase hexadecimal user ID")
		}
		// permission_flags bit 0 is upload. Creating the row lets a user be
		// granted the permission before their first sign-in.
		_, err = pool.Exec(ctx, `INSERT INTO users(id,permission_flags) VALUES($1,CASE WHEN $2 THEN 1 ELSE 0 END)
 ON CONFLICT(id) DO UPDATE SET permission_flags=CASE WHEN $2 THEN users.permission_flags | 1 ELSE users.permission_flags & ~1::bigint END`, id, *grant != "")
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("upload permission of %s set to %t", id, *grant != "")
		return
	}
	cancel()
	listen := env("LISTEN_ADDR", "127.0.0.1:8081")
	var sso *httpapi.SSOClient
	devUser := os.Getenv("DEV_USER_ID")
	if os.Getenv("SSO_ISSUER") != "" {
		if devUser != "" {
			log.Fatal("DEV_USER_ID cannot be combined with SSO_ISSUER")
		}
		sso, err = httpapi.NewSSO(httpapi.SSOConfig{Issuer: os.Getenv("SSO_ISSUER"), AvatarBaseURL: os.Getenv("SSO_AVATAR_BASE_URL"), ConnectAddress: os.Getenv("SSO_CONNECT_ADDRESS"), ServiceID: os.Getenv("SSO_SERVICE_ID"), ServiceKey: os.Getenv("SSO_SERVICE_KEY"), ClientID: os.Getenv("SSO_CLIENT_ID"), ClientSecret: os.Getenv("SSO_CLIENT_SECRET"), RedirectURL: os.Getenv("SSO_REDIRECT_URL"), EncryptionKey: os.Getenv("SESSION_ENCRYPTION_KEY")})
		if err != nil {
			log.Fatal(err)
		}
	} else if devUser != "" {
		if !validUserID(devUser) || !loopback(listen) {
			log.Fatal("DEV_USER_ID needs a 32-character lowercase hexadecimal ID and a loopback LISTEN_ADDR")
		}
		log.Printf("development sign-in enabled for user %s; never use this outside local development", devUser)
	} else {
		log.Print("no SSO_ISSUER: the catalogue is public, sign-in and uploads are unavailable")
	}
	proxies, err := httpapi.ParseTrustedProxies(os.Getenv("TRUSTED_PROXY_CIDRS"))
	if err != nil {
		log.Fatal(err)
	}
	// In S3 mode STORAGE_DIR only stages uploads.
	storage := env("STORAGE_DIR", ".data/files")
	objects, err := objectstore.FromEnv(storage)
	if err != nil {
		log.Fatal(err)
	}
	var profile *httpapi.ProfileClient
	if os.Getenv("PROFILE_URL") != "" {
		profile, err = httpapi.NewProfile(httpapi.ProfileConfig{URL: os.Getenv("PROFILE_URL"), ServiceID: os.Getenv("PROFILE_SERVICE_ID"), ServiceKey: os.Getenv("PROFILE_SERVICE_KEY")})
		if err != nil {
			log.Fatal(err)
		}
	}
	site := webapp.Site()
	if site == nil {
		log.Print("website not built: run pnpm build in web; serving API only")
	}
	// TJAParser reads chart bodies for every upload and the content-hash backfill; a missing
	// or broken tool is a deployment error, found now rather than at the first upload.
	tja.Inspector = charthash.CLI{Bin: env("TJAPARSER_BIN", "tjaparser")}
	tja.InspectDir = storage
	// Its own deadline: the start-up context above is not for this.
	toolCtx, cancelTool := context.WithTimeout(context.Background(), 30*time.Second)
	version, err := tja.Inspector.(charthash.CLI).Version(toolCtx)
	cancelTool()
	if err != nil {
		log.Fatalf("tjaparser is not usable: %v", err)
	}
	log.Printf("tjaparser hash algorithm %s", version)
	app := httpapi.New(pool, httpapi.Config{Site: site, Profile: profile, SSO: sso, DevUserID: devUser, TrustedProxies: proxies, Origin: env("APP_ORIGIN", "http://127.0.0.1:5174"), Storage: storage, Objects: objects, CookieSecure: env("COOKIE_SECURE", "false") == "true"})
	if err = app.EnsureStorage(); err != nil {
		log.Fatal(err)
	}
	// Staged files do not survive a restart: uploads still being processed are reported failed.
	if err = app.FailInterruptedUploads(context.Background()); err != nil {
		log.Fatal(err)
	}
	background, stopBackground := context.WithCancel(context.Background())
	defer stopBackground()
	go app.RunNotificationPrune(background)
	go app.RunDifficultyConstants(background)
	go app.RunPreviewBackfill(background)
	srv := &http.Server{Addr: listen, Handler: app.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 180 * time.Second, WriteTimeout: 240 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 * 1024}
	go func() {
		log.Printf("OurTaikoDatabase listening on http://%s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	srv.Shutdown(shutdown)
}
