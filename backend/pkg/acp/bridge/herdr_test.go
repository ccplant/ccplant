package bridge

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHerdrRuntimeRequiresOptIn(t *testing.T) {
	for _, enabled := range []string{"", "1"} {
		t.Run("enabled="+enabled, func(t *testing.T) {
			t.Setenv("AGENTAPI_HERDR_ENABLED", enabled)
			s := NewServer(New(nil, "test", false, "", false), false)
			w := httptest.NewRecorder()
			// Invalid target requires no running Herdr and performs no socket IO.
			s.echo.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/herdr/output", nil))
			want := http.StatusNotFound
			if enabled == "1" {
				want = http.StatusConflict
			}
			require.Equal(t, want, w.Code)
		})
	}
}
