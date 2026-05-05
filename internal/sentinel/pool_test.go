package sentinel

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

type testPeer struct {
	ID        string `json:"id"`
	ProxyPath string `json:"proxy_path"`
	Healthy   bool   `json:"healthy"`
	Pool      string `json:"pool,omitempty"`
}

// TestPeersHandlerPoolFilter verifies that pool tags propagate from the
// backend through /sentinel/peers and that the ?pool= query param filters
// correctly while preserving back-compat (no param = return everything).
func TestPeersHandlerPoolFilter(t *testing.T) {
	m := &Manager{backends: NewBackendPool()}

	m.backends.Add(&Backend{ID: "tunnel-a", Type: BackendTunnel, Healthy: true, Pool: "prod"})
	m.backends.Add(&Backend{ID: "tunnel-b", Type: BackendTunnel, Healthy: true, Pool: "dev"})
	m.backends.Add(&Backend{ID: "tunnel-c", Type: BackendTunnel, Healthy: false, Pool: ""})
	m.backends.Add(&Backend{ID: "gcp-x", Type: BackendGCP, Healthy: true, Pool: "prod"})

	call := func(query string) []testPeer {
		t.Helper()
		req := httptest.NewRequest("GET", "/sentinel/peers"+query, nil)
		rec := httptest.NewRecorder()
		m.PeersHandler()(rec, req)
		assert.Equal(t, 200, rec.Code)
		var resp struct {
			Peers []testPeer `json:"peers"`
		}
		assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		return resp.Peers
	}

	t.Run("no filter returns all tunnel peers (back-compat)", func(t *testing.T) {
		peers := call("")
		ids := idsOf(peers)
		assert.ElementsMatch(t, []string{"tunnel-a", "tunnel-b", "tunnel-c"}, ids)
		// GCP backends are never returned.
		assert.NotContains(t, ids, "gcp-x")
	})

	t.Run("filter by pool=prod returns only prod peers", func(t *testing.T) {
		peers := call("?pool=prod")
		assert.Len(t, peers, 1)
		assert.Equal(t, "tunnel-a", peers[0].ID)
		assert.Equal(t, "prod", peers[0].Pool)
	})

	t.Run("filter by pool=dev returns only dev peers", func(t *testing.T) {
		peers := call("?pool=dev")
		assert.Len(t, peers, 1)
		assert.Equal(t, "tunnel-b", peers[0].ID)
	})

	t.Run("empty pool value matches unpooled peers only", func(t *testing.T) {
		peers := call("?pool=")
		assert.Len(t, peers, 1)
		assert.Equal(t, "tunnel-c", peers[0].ID)
		assert.Equal(t, "", peers[0].Pool)
	})

	t.Run("unknown pool returns empty list", func(t *testing.T) {
		peers := call("?pool=ghost")
		assert.Empty(t, peers)
	})
}

// TestRegisterPropagatesPool confirms the pool tag flows from Register()
// into the TunnelSpot.
func TestRegisterPropagatesPool(t *testing.T) {
	r := NewTunnelRegistry()
	_, err := r.Register("spot-1", nil, []int{8080}, "prod")
	assert.NoError(t, err)

	spot := r.Get("spot-1")
	assert.NotNil(t, spot)
	assert.Equal(t, "prod", spot.Pool)

	// Empty pool stays empty (back-compat).
	_, err = r.Register("spot-2", nil, []int{8080}, "")
	assert.NoError(t, err)
	assert.Equal(t, "", r.Get("spot-2").Pool)
}

func idsOf(peers []testPeer) []string {
	out := make([]string, 0, len(peers))
	for _, p := range peers {
		out = append(out, p.ID)
	}
	return out
}
