package handlers_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bilelzarai/siraj/internal/config"
	"github.com/bilelzarai/siraj/internal/database"
	"github.com/bilelzarai/siraj/internal/handlers"
	"github.com/bilelzarai/siraj/internal/i18n"
	"github.com/bilelzarai/siraj/internal/repository"
	"github.com/bilelzarai/siraj/internal/service"
)

// A real server, a real database, a real browser session — every screen walked
// the way somebody uses it. The unit tests check pieces; this checks that the
// pieces are wired to each other, which is the failure nobody notices until a
// page 500s in front of a user.
//
// Everything runs against a database this test builds and drops, so it never
// touches development data.

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	url := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if url == "" {
		url = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	}
	if url == "" {
		url = dotenvURL()
	}
	if url == "" {
		fmt.Println("handler walk skipped: no database URL")
		os.Exit(m.Run())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	name := fmt.Sprintf("siraj_walk_%d", os.Getpid())
	admin, err := pgxpool.New(ctx, url)
	if err != nil || admin.Ping(ctx) != nil {
		fmt.Println("handler walk skipped: database unreachable")
		os.Exit(m.Run())
	}
	if _, err := admin.Exec(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		fmt.Println("handler walk skipped:", err)
		admin.Close()
		os.Exit(m.Run())
	}
	defer func() {
		_, _ = admin.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, name)
		_, _ = admin.Exec(ctx, `DROP DATABASE IF EXISTS "`+name+`"`)
		admin.Close()
	}()

	pool, err := pgxpool.New(ctx, swapDB(url, name))
	if err != nil {
		fmt.Println("handler walk skipped:", err)
		os.Exit(m.Run())
	}
	if err := database.Migrate(ctx, pool); err != nil {
		fmt.Println("handler walk skipped:", err)
		os.Exit(m.Run())
	}
	testPool = pool
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

// app is a running instance plus a browser that keeps its cookies.
type app struct {
	t      *testing.T
	server *httptest.Server
	client *http.Client
	repo   *repository.Repo
}

func newApp(t *testing.T) *app { return newAppWithSMTP(t, "") }

// newAppWithSMTP points the mailer at a server this test controls. With an
// empty address the mailer is disabled and logs instead, which is what
// development without a mail server does.
func newAppWithSMTP(t *testing.T, smtpAddr string) *app {
	t.Helper()
	if testPool == nil {
		t.Skip("no test database")
	}

	cfg := &config.Config{
		Env:             "test",
		BaseURL:         "http://localhost",
		DefaultLocale:   "en",
		SessionSecret:   "test-secret-that-is-long-enough-for-hmac",
		SessionLifetime: time.Hour,
		SecureCookies:   false,
		// Somewhere to put uploads that goes away with the test.
		UploadDir:          t.TempDir(),
		MaxUploadBytes:     8 << 20,
		MaxFilesPerMessage: 5,
	}
	if smtpAddr != "" {
		host, port, err := net.SplitHostPort(smtpAddr)
		if err != nil {
			t.Fatalf("smtp address %q: %v", smtpAddr, err)
		}
		p, _ := strconv.Atoi(port)
		cfg.SMTP = config.SMTPConfig{
			Host: host, Port: p, FromEmail: "test@siraj.local",
			FromName: "Sirāj", StartTLS: false,
		}
		cfg.BaseURL = "http://localhost:8080"
	}
	bundle, err := i18n.New("en")
	if err != nil {
		t.Fatalf("i18n: %v", err)
	}

	repo := repository.New(testPool)
	hub := service.NewHub()
	h := handlers.New(cfg, repo, bundle,
		service.NewAuth(repo, cfg.SessionLifetime, cfg.SecureCookies),
		service.NewGame(repo, hub), service.NewSocial(repo, hub), hub,
		service.NewPresence(repo), service.NewImporter(repo),
		service.NewTranslator(cfg.Translate), service.NewReset(repo),
		service.NewMailer(cfg.SMTP))

	server := httptest.NewServer(h.Routes(os.DirFS(repoRoot(t) + "/static")))
	t.Cleanup(server.Close)

	jar, _ := cookiejar.New(nil)
	return &app{
		t: t, server: server, repo: repo,
		client: &http.Client{
			Jar: jar,
			// Follow nothing: a redirect is an answer, and which one it is
			// matters more than where it lands.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// newAppSharing is a second browser against the same running server, for the
// flows that need two people.
func newAppSharing(t *testing.T, other *app) *app {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	return &app{
		t: t, server: other.server, repo: other.repo,
		client: &http.Client{
			Jar: jar,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (a *app) get(path string) (int, string) {
	a.t.Helper()
	res, err := a.client.Get(a.server.URL + path)
	if err != nil {
		a.t.Fatalf("GET %s: %v", path, err)
	}
	defer res.Body.Close()
	body := readAll(res)
	return res.StatusCode, body
}

func (a *app) post(path string, form url.Values) (int, string) {
	a.t.Helper()
	form.Set("csrf_token", a.csrf())
	res, err := a.client.PostForm(a.server.URL+path, form)
	if err != nil {
		a.t.Fatalf("POST %s: %v", path, err)
	}
	defer res.Body.Close()
	return res.StatusCode, res.Header.Get("Location")
}

// postBody is post for the routes the script calls rather than the ones a form
// submits to: they answer in JSON where a form would be sent somewhere.
func (a *app) postBody(path string, form url.Values) (int, string) {
	a.t.Helper()
	if form == nil {
		form = url.Values{}
	}
	form.Set("csrf_token", a.csrf())
	res, err := a.client.PostForm(a.server.URL+path, form)
	if err != nil {
		a.t.Fatalf("POST %s: %v", path, err)
	}
	defer res.Body.Close()
	return res.StatusCode, readAll(res)
}

// csrf reads the token the server issued, the way the form in the page does.
func (a *app) csrf() string {
	u, _ := url.Parse(a.server.URL)
	for _, c := range a.client.Jar.Cookies(u) {
		if c.Name == "siraj_csrf" {
			return c.Value
		}
	}
	return ""
}

func readAll(res *http.Response) string {
	buf := make([]byte, 1<<20)
	n, _ := res.Body.Read(buf)
	total := n
	for n > 0 && total < len(buf) {
		n, _ = res.Body.Read(buf[total:])
		total += n
	}
	return string(buf[:total])
}

func swapDB(raw, name string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Path = "/" + name
	return u.String()
}

func dotenvURL() string {
	dir, _ := os.Getwd()
	for i := 0; i < 6; i++ {
		body, err := os.ReadFile(filepath.Join(dir, ".env"))
		if err == nil {
			for _, line := range strings.Split(string(body), "\n") {
				if after, ok := strings.CutPrefix(strings.TrimSpace(line), "DATABASE_URL="); ok {
					return strings.Trim(strings.TrimSpace(after), `"'`)
				}
			}
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	return "."
}
