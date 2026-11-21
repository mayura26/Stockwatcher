package push

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	webpush "github.com/SherClockHolmes/webpush-go"

	"stockwatcher/internal/store"
)

// Note is one price-cross notification.
type Note struct {
	Title    string
	Body     string
	SymbolID int64
	AlertID  int64
}

// Sender delivers Web Push to every stored subscription.
type Sender struct {
	Store      *store.Store
	Public     string
	Private    string
	Subscriber string
}

func (s *Sender) Enabled() bool {
	return s != nil && s.Public != "" && s.Private != "" && s.Subscriber != ""
}

// Notify sends to all subscriptions. 404 and 410 drop the subscription.
// A failed send does not un-trigger the alert.
func (s *Sender) Notify(ctx context.Context, note Note) {
	if s == nil || s.Store == nil {
		return
	}
	if !s.Enabled() {
		slog.Warn("push skipped, vapid is not configured", "title", note.Title)
		return
	}
	subs, err := s.Store.Subscriptions(ctx)
	if err != nil {
		slog.Error("list subscriptions", "err", err)
		return
	}
	if len(subs) == 0 {
		slog.Info("push skipped, no subscriptions", "title", note.Title)
		return
	}
	payload, err := json.Marshal(map[string]any{
		"title":     note.Title,
		"body":      note.Body,
		"symbol_id": note.SymbolID,
		"alert_id":  note.AlertID,
	})
	if err != nil {
		slog.Error("push payload", "err", err)
		return
	}
	for _, sub := range subs {
		resp, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
			Endpoint: sub.Endpoint,
			Keys:     webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth},
		}, &webpush.Options{
			Subscriber:      s.Subscriber,
			VAPIDPublicKey:  s.Public,
			VAPIDPrivateKey: s.Private,
			TTL:             60,
			Urgency:         webpush.UrgencyHigh,
		})
		if err != nil {
			slog.Error("push", "err", err)
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
			if err := s.Store.DeleteSubscription(ctx, sub.Endpoint); err != nil {
				slog.Error("drop subscription", "err", err)
			}
			continue
		}
		if resp.StatusCode >= 300 {
			slog.Error("push status", "code", resp.StatusCode, "endpoint", hostOf(sub.Endpoint))
		}
	}
}

func hostOf(endpoint string) string {
	if i := strings.Index(endpoint, "://"); i >= 0 {
		endpoint = endpoint[i+3:]
	}
	if i := strings.IndexByte(endpoint, '/'); i >= 0 {
		return endpoint[:i]
	}
	return endpoint
}
