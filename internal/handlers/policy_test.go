package handlers

import (
	"strings"
	"testing"

	"github.com/bilelzarai/siraj/internal/config"
)

// The content policy became a function of configuration so the asset dev
// server could be admitted on a laptop. This is the test that stops that
// widening from reaching a deployment: the production string is built *with* a
// dev origin configured, and it has to come back the bytes it has always been.
//
// Without it the failure mode is silent — a page that works everywhere and a
// policy that stopped protecting anybody.
func TestTheProductionPolicyIgnoresTheDevServer(t *testing.T) {
	const dev = "http://localhost:5173"

	production := contentSecurityPolicy(&config.Config{Env: "production", ViteDevServer: dev})

	if strings.Contains(production, dev) || strings.Contains(production, "localhost") {
		t.Errorf("the production policy admits the dev origin:\n%s", production)
	}
	if strings.Contains(production, "unsafe-eval") {
		t.Errorf("the production policy grants an evaluation exception:\n%s", production)
	}
	if strings.Contains(production, "ws://") || strings.Contains(production, "wss://") {
		t.Errorf("the production policy admits a websocket origin:\n%s", production)
	}

	// Byte-identical to a production deployment that was never told about a dev
	// server, which is the only way to be sure nothing leaked in.
	unconfigured := contentSecurityPolicy(&config.Config{Env: "production"})
	if production != unconfigured {
		t.Errorf("configuring a dev server changed the production policy:\n got %s\nwant %s",
			production, unconfigured)
	}

	// And the whole point of the change still works.
	development := contentSecurityPolicy(&config.Config{Env: "development", ViteDevServer: dev})
	if !strings.Contains(development, dev) {
		t.Errorf("development does not admit the dev origin, so replacement cannot work:\n%s", development)
	}
	if !strings.Contains(development, "ws://localhost:5173") {
		t.Errorf("development does not admit the dev websocket:\n%s", development)
	}
	if strings.Contains(development, "unsafe-eval") {
		t.Errorf("development grants an evaluation exception; the policy-safe build needs none:\n%s", development)
	}
}
