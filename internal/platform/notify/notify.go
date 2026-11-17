// Package notify sends push notifications through Firebase Cloud Messaging
// (HTTP v1 API) using a service account.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// ErrUnregistered means the device token is no longer valid and should be deleted.
var ErrUnregistered = errors.New("fcm token unregistered")

// Message is a notification to one device.
type Message struct {
	Title string
	Body  string
	Data  map[string]string
}

// Sender delivers a message to a single device token.
type Sender interface {
	Send(ctx context.Context, token string, m Message) error
}

// FCM sends through the Firebase HTTP v1 API.
type FCM struct {
	client *http.Client
	url    string
}

// NewFCM builds a sender from a Firebase service account JSON key.
func NewFCM(ctx context.Context, serviceAccountJSON []byte) (*FCM, error) {
	creds, err := google.CredentialsFromJSON(ctx, serviceAccountJSON, "https://www.googleapis.com/auth/firebase.messaging")
	if err != nil {
		return nil, fmt.Errorf("fcm credentials: %w", err)
	}
	if creds.ProjectID == "" {
		return nil, errors.New("fcm credentials: project_id missing")
	}
	return &FCM{
		client: oauth2.NewClient(ctx, creds.TokenSource),
		url:    fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", creds.ProjectID),
	}, nil
}

// NewFCMWithClient is for tests: it sends to url using client as is.
func NewFCMWithClient(client *http.Client, url string) *FCM { return &FCM{client: client, url: url} }

func (f *FCM) Send(ctx context.Context, token string, m Message) error {
	payload := map[string]any{"message": map[string]any{
		"token":        token,
		"notification": map[string]string{"title": m.Title, "body": m.Body},
		"data":         m.Data,
	}}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return fmt.Errorf("fcm request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))

	switch {
	case resp.StatusCode/100 == 2:
		return nil
	case resp.StatusCode == http.StatusNotFound:
		return ErrUnregistered
	case resp.StatusCode == http.StatusBadRequest && bytes.Contains(raw, []byte("INVALID_ARGUMENT")):
		return ErrUnregistered
	default:
		return fmt.Errorf("fcm status %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
}

// Noop is used when FCM is not configured. It never fails.
type Noop struct{ Log *slog.Logger }

func (n Noop) Send(ctx context.Context, _ string, m Message) error {
	if n.Log != nil {
		n.Log.InfoContext(ctx, "push skipped: FCM not configured", "title", m.Title)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
