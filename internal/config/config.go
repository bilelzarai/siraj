package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds every runtime knob, resolved from the environment.
type Config struct {
	Env             string
	Addr            string
	BaseURL         string
	DatabaseURL     string
	SessionSecret   string
	SessionLifetime time.Duration
	SecureCookies   bool
	DefaultLocale   string
	Seed            bool

	// TrustedProxyHops is how many reverse proxies sit in front of this
	// process. Zero — the default — means the peer address is taken from the
	// connection and X-Forwarded-For is ignored entirely, because a header
	// anyone can set must not decide who the rate limiter is throttling.
	TrustedProxyHops int

	// UploadDir is where sent files land. On disk rather than in the database:
	// a thread of photographs would otherwise turn every backup into a media
	// library. Empty disables uploads entirely, which is what a deployment
	// with no writable volume wants.
	UploadDir      string
	MaxUploadBytes int64
	// MaxFilesPerMessage is how many attachments one send may carry. Picking
	// six photographs and having five arrive is worse than being told five is
	// the limit, so the number is answered before anything is stored.
	MaxFilesPerMessage int

	SMTP      SMTPConfig
	Translate TranslateConfig
}

// SMTPConfig drives outbound mail. With Host empty the mailer logs instead of
// sending, so development works with nothing configured.
type SMTPConfig struct {
	Host      string
	Port      int
	Username  string
	Password  string
	FromEmail string
	FromName  string
	StartTLS  bool
}

func (s SMTPConfig) Enabled() bool { return s.Host != "" && s.FromEmail != "" }

// TranslateConfig selects the machine-translation backend. Provider "none"
// (the default) means content must be translated by hand.
type TranslateConfig struct {
	Provider string // none | claude | libretranslate

	AnthropicKey   string
	AnthropicModel string

	LibreURL string
	LibreKey string
}

// Load reads .env (if present) and builds the config, failing loudly on
// anything that would produce a silently broken server.
func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		Env:             env("APP_ENV", "development"),
		Addr:            env("APP_ADDR", ":8080"),
		BaseURL:         strings.TrimRight(env("BASE_URL", ""), "/"),
		DatabaseURL:     env("DATABASE_URL", ""),
		SessionSecret:   env("SESSION_SECRET", ""),
		SessionLifetime: envDuration("SESSION_LIFETIME", 30*24*time.Hour),
		SecureCookies:   envBool("SECURE_COOKIES", false),
		DefaultLocale:   env("DEFAULT_LOCALE", "ar"),
		// Off by default: loading the question bank is a deliberate one-off
		// (sirajctl seed), not something a restart should decide.
		Seed: envBool("SEED_ON_START", false),

		TrustedProxyHops: envInt("TRUSTED_PROXY_HOPS", 0),

		UploadDir:      env("UPLOAD_DIR", "data/uploads"),
		MaxUploadBytes: int64(envInt("MAX_UPLOAD_MB", 8)) * 1 << 20,

		MaxFilesPerMessage: envInt("MAX_FILES_PER_MESSAGE", 5),

		SMTP: SMTPConfig{
			Host:      env("SMTP_HOST", ""),
			Port:      envInt("SMTP_PORT", 587),
			Username:  env("SMTP_USERNAME", ""),
			Password:  env("SMTP_PASSWORD", ""),
			FromEmail: env("SMTP_FROM_EMAIL", ""),
			FromName:  env("SMTP_FROM_NAME", "Sirāj"),
			StartTLS:  envBool("SMTP_STARTTLS", true),
		},

		Translate: TranslateConfig{
			Provider:       env("TRANSLATE_PROVIDER", "none"),
			AnthropicKey:   env("ANTHROPIC_API_KEY", ""),
			AnthropicModel: env("ANTHROPIC_MODEL", "claude-opus-5"),
			LibreURL:       strings.TrimRight(env("LIBRETRANSLATE_URL", ""), "/"),
			LibreKey:       env("LIBRETRANSLATE_API_KEY", ""),
		},
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	if cfg.BaseURL == "" {
		// Invite links need an absolute URL; derive a usable default locally.
		host := cfg.Addr
		if strings.HasPrefix(host, ":") {
			host = "localhost" + host
		}
		cfg.BaseURL = "http://" + host
		if cfg.IsProduction() {
			return nil, fmt.Errorf("BASE_URL is required in production (used for invite links)")
		}
	}

	switch cfg.Translate.Provider {
	case "", "none":
		cfg.Translate.Provider = "none"
	case "claude":
		if cfg.Translate.AnthropicKey == "" {
			return nil, fmt.Errorf("TRANSLATE_PROVIDER=claude requires ANTHROPIC_API_KEY")
		}
	case "libretranslate":
		if cfg.Translate.LibreURL == "" {
			return nil, fmt.Errorf("TRANSLATE_PROVIDER=libretranslate requires LIBRETRANSLATE_URL")
		}
	default:
		return nil, fmt.Errorf("unknown TRANSLATE_PROVIDER %q (want none, claude or libretranslate)",
			cfg.Translate.Provider)
	}

	// Zero would be a composer whose attach button can never produce a send,
	// and the ceiling on the request body is derived from this.
	if cfg.MaxFilesPerMessage < 1 {
		return nil, fmt.Errorf("MAX_FILES_PER_MESSAGE must be at least 1 (got %d)", cfg.MaxFilesPerMessage)
	}

	if cfg.TrustedProxyHops < 0 {
		return nil, fmt.Errorf("TRUSTED_PROXY_HOPS cannot be negative (got %d)", cfg.TrustedProxyHops)
	}

	if cfg.SessionSecret == "" {
		if cfg.IsProduction() {
			return nil, fmt.Errorf("SESSION_SECRET is required in production")
		}
		// Dev convenience: a throwaway secret so the server still boots.
		// Flashes signed before a restart stop verifying, which is fine locally.
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			return nil, err
		}
		cfg.SessionSecret = hex.EncodeToString(buf)
	} else if len(cfg.SessionSecret) < 32 {
		// A short secret is worse than no secret: it boots, looks configured,
		// and is brute-forceable. Refuse it everywhere, not only in production,
		// so the mistake is caught on the developer's machine.
		return nil, fmt.Errorf("SESSION_SECRET must be at least 32 characters (got %d)",
			len(cfg.SessionSecret))
	}

	if cfg.IsProduction() {
		// Session cookies without Secure travel in clear over any plain-HTTP
		// hop. Forgetting one environment variable must not silently downgrade
		// every session on the deployment.
		if !cfg.SecureCookies {
			return nil, fmt.Errorf("SECURE_COOKIES must be true in production; " +
				"set SECURE_COOKIES=true once the site is served over HTTPS")
		}
		if !strings.HasPrefix(cfg.BaseURL, "https://") {
			return nil, fmt.Errorf("BASE_URL must be https:// in production (got %q); "+
				"password-reset and invite links are sent by email", cfg.BaseURL)
		}
	}

	return cfg, nil
}

func (c *Config) IsProduction() bool { return c.Env == "production" }

// FilesPerMessage is MaxFilesPerMessage with a floor under it. Load refuses a
// configured zero, but a Config assembled in code has one by default, and a
// zero here is a composer whose attach button can never produce a send —
// every file refused as one too many.
func (c *Config) FilesPerMessage() int {
	if c.MaxFilesPerMessage < 1 {
		return 1
	}
	return c.MaxFilesPerMessage
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
