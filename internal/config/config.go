// Package config turns environment variables into a typed Config.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env         string        // development | production
	HTTPAddr    string        // e.g. :8080
	DatabaseURL string        // postgres://user:pass@host:5432/db?sslmode=disable
	SessionTTL  time.Duration // how long a session token stays valid
	GuestTTL    time.Duration // how long a guest account lives

	BaseURL       string // public URL, used in emails and share links
	ViteDevServer string // http://localhost:5173 in development, empty in production
	StorageDriver string // local | s3  (decision D-02)
	UploadsDir    string // where the local driver writes

	SMTPAddr       string // host:port of a mail server; empty logs the mails instead
	MailFrom       string
	GoogleClientID string // sign in with Google (endpoint 3); empty disables the button
	AppleClientID  string // sign in with Apple; empty disables the button
	SMSProvider    string // gateway for the one-time codes; empty logs them instead

	// CORSOrigins are the origins allowed to call /api/v1 from a browser: a mobile
	// app does not need them, a web client on another host does. Empty means no
	// cross-origin call is answered; "*" allows any (development only).
	CORSOrigins []string
}

func (c Config) IsDev() bool { return c.Env != "production" }

// Load reads .env (if present, without overriding real env vars) and then the environment.
func Load() (Config, error) {
	loadDotEnv(".env")
	c := Config{
		Env:         get("APP_ENV", "development"),
		HTTPAddr:    get("HTTP_ADDR", ":8080"),
		DatabaseURL: get("DATABASE_URL", ""),
		SessionTTL:  time.Duration(getInt("SESSION_TTL_HOURS", 24*30)) * time.Hour,
		GuestTTL:    time.Duration(getInt("GUEST_TTL_HOURS", 24)) * time.Hour,

		BaseURL:       get("BASE_URL", "http://localhost:8080"),
		ViteDevServer: get("VITE_DEV_SERVER", ""),
		StorageDriver: get("STORAGE_DRIVER", "local"),
		UploadsDir:    get("UPLOADS_DIR", "./var/uploads"),

		SMTPAddr:       get("SMTP_ADDR", ""),
		MailFrom:       get("MAIL_FROM", "Siraj <no-reply@siraj.app>"),
		GoogleClientID: get("GOOGLE_CLIENT_ID", ""),
		AppleClientID:  get("APPLE_CLIENT_ID", ""),
		SMSProvider:    get("SMS_PROVIDER", ""),
		CORSOrigins:    list(get("CORS_ORIGINS", "")),
	}
	if c.Env == "production" {
		c.ViteDevServer = "" // production always serves the built assets
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL is not set (copy .env.example to .env)")
	}
	return c, nil
}

// list splits a comma-separated variable, dropping the blanks around it.
func list(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func get(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getInt(key string, def int) int {
	if v, err := strconv.Atoi(get(key, "")); err == nil {
		return v
	}
	return def
}

// loadDotEnv is a tiny KEY=VALUE reader so the server runs the same from VS Code, air or
// a terminal. It walks up a few directories, so `go test ./tests/...` finds the same file.
func loadDotEnv(name string) {
	path := name
	for i := 0; i < 4; i++ {
		if _, err := os.Stat(path); err == nil {
			break
		}
		path = filepath.Join("..", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`)
		if _, exists := os.LookupEnv(k); !exists {
			os.Setenv(k, v)
		}
	}
}
