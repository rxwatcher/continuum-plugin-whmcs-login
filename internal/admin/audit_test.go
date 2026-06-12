package admin_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/go-hclog"

	"github.com/RXWatcher/silo-plugin-whmcs-login/internal/admin"
	pluginrt "github.com/RXWatcher/silo-plugin-whmcs-login/internal/runtime"
)

// syncBuffer is a goroutine-safe buffer for capturing log output.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestHandleUpdateConfig_AuditsMutationWithActorAndFields(t *testing.T) {
	var logged syncBuffer
	log := hclog.New(&hclog.LoggerOptions{Output: &logged, Level: hclog.Info})

	var saved pluginrt.Config
	s := admin.NewServer(admin.Deps{
		ConfigFn: func() pluginrt.Config { return pluginrt.Config{WHMCSServerURL: "https://x"} },
		UpdateConfigFn: func(_ context.Context, c pluginrt.Config) error {
			saved = c
			return nil
		},
		Logger: log,
	})

	body := `{"display_name":"My Login","fetch_discord_id":true}`
	r := httptest.NewRequest("PATCH", "/api/v1/admin/config", strings.NewReader(body))
	r.Header.Set("X-Silo-User-Role", "admin")
	r.Header.Set("X-Silo-User-Id", "admin-42")
	w := httptest.NewRecorder()
	mountRouter(s).ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d (body=%s)", w.Code, w.Body.String())
	}
	if saved.DisplayName != "My Login" || !saved.FetchDiscordID {
		t.Errorf("saved config = %+v", saved)
	}
	out := logged.String()
	if !strings.Contains(out, "admin config updated") {
		t.Errorf("missing audit line: %s", out)
	}
	if !strings.Contains(out, "admin-42") {
		t.Errorf("audit line missing actor: %s", out)
	}
	if !strings.Contains(out, "display_name") || !strings.Contains(out, "fetch_discord_id") {
		t.Errorf("audit line missing changed fields: %s", out)
	}
}
