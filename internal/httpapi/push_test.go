package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"stockwatcher/internal/config"
	"stockwatcher/internal/push"
	"stockwatcher/internal/store"
	"stockwatcher/internal/syncer"
)

func TestPushTestStatus(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	unconfigured := New(st, syncer.New(st, nil, nil, nil, nil), config.Config{AccessToken: "testtoken"})
	rec := postPushTest(unconfigured.Handler())
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured status %d %s", rec.Code, rec.Body.String())
	}
	if msg := errorMessage(t, rec); msg != "Push is not configured" {
		t.Fatalf("unconfigured message %q", msg)
	}

	sender := &push.Sender{Store: st, Public: "pub", Private: "priv", Subscriber: "mailto:a@b.c"}
	configured := New(st, syncer.New(st, nil, nil, nil, sender), config.Config{AccessToken: "testtoken"})
	rec = postPushTest(configured.Handler())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty status %d %s", rec.Code, rec.Body.String())
	}
	if msg := errorMessage(t, rec); msg != "No push subscriptions" {
		t.Fatalf("empty message %q", msg)
	}
}

func postPushTest(h http.Handler) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/push/test", nil)
	req.Header.Set("Authorization", "Bearer testtoken")
	h.ServeHTTP(rec, req)
	return rec
}

func errorMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Error
}
