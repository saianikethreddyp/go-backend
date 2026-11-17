package notifications

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/saianikethreddyp/thandra-api/internal/platform/notify"
)

type fakeRepo struct {
	tokens  []string
	deleted []string
}

func (f *fakeRepo) Upsert(context.Context, uuid.UUID, string, string) error { return nil }
func (f *fakeRepo) Delete(context.Context, uuid.UUID, string) error         { return nil }
func (f *fakeRepo) Tokens(context.Context, uuid.UUID) ([]string, error)     { return f.tokens, nil }
func (f *fakeRepo) DeleteTokens(_ context.Context, t []string) error {
	f.deleted = append(f.deleted, t...)
	return nil
}

type fakeSender struct {
	sent []string
	fail map[string]error
}

func (f *fakeSender) Send(_ context.Context, token string, _ notify.Message) error {
	if err := f.fail[token]; err != nil {
		return err
	}
	f.sent = append(f.sent, token)
	return nil
}

func TestDeliverPrunesDeadTokensAndCountsSent(t *testing.T) {
	repo := &fakeRepo{tokens: []string{"good-token", "dead-token", "flaky-token"}}
	sender := &fakeSender{fail: map[string]error{
		"dead-token":  notify.ErrUnregistered,
		"flaky-token": errors.New("temporary outage"),
	}}
	svc := NewService(repo, sender, slog.New(slog.NewTextHandler(io.Discard, nil)))

	n, err := svc.deliver(context.Background(), uuid.New(), "t", "b", nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(sender.sent) != 1 || sender.sent[0] != "good-token" {
		t.Fatalf("sent = %d %v", n, sender.sent)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != "dead-token" {
		t.Fatalf("only the unregistered token should be pruned, got %v", repo.deleted)
	}
}

func TestNotifyUserRunsInBackground(t *testing.T) {
	repo := &fakeRepo{tokens: []string{"good-token"}}
	sender := &fakeSender{}
	svc := NewService(repo, sender, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.NotifyUser(context.Background(), uuid.New(), "t", "b", nil)
	svc.Wait()
	if len(sender.sent) != 1 {
		t.Fatalf("expected one push, got %v", sender.sent)
	}
}
