package main

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/statuspage"
)

// `anubis config show` must mask the SMTP relay password like every other
// credential, in YAML and JSON configs.
func TestConfigShowMasksMailPassword(t *testing.T) {
	yaml := "mail:\n  host: smtp.example.com\n  username: anubis@example.com\n  password: hunter2-mail-secret\n  from: status@example.com\n"
	json := `{"mail": {"host": "smtp.example.com", "password": "hunter2-mail-secret"}}`
	for _, text := range []string{yaml, json} {
		out := redactConfigSecrets(text)
		if strings.Contains(out, "hunter2-mail-secret") {
			t.Fatalf("mail password shown:\n%s", out)
		}
		if !strings.Contains(out, "smtp.example.com") {
			t.Fatalf("non-secret mail host masked:\n%s", out)
		}
	}
	if out := redactConfigSecrets("mail:\n  password: ${ANUBIS_MAIL_PASSWORD}\n"); !strings.Contains(out, "${ANUBIS_MAIL_PASSWORD}") {
		t.Fatalf("env reference hidden:\n%s", out)
	}
}

func TestNewMailQueueOnlyWhenConfigured(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	q, err := newMailQueue(&core.Config{}, logger)
	if q != nil || err != nil {
		t.Fatalf("unconfigured mail = %v, %v", q, err)
	}
	cfg := &core.Config{Mail: core.MailConfig{Host: "smtp.example.com", From: "status@example.com"}}
	q, err = newMailQueue(cfg, logger)
	if err != nil || q == nil {
		t.Fatalf("configured mail = %v, %v", q, err)
	}
	q.Close()
}

func TestIncidentHookAddsSubscribersOnlyWhenEnabled(t *testing.T) {
	calls := 0
	live := func(*core.Incident) { calls++ }
	plain := statuspage.NewHandler(&statusPageRepository{})
	incidentHook(live, plain)(&core.Incident{ID: "i"})
	incidentHook(live, nil)(&core.Incident{ID: "i"})
	if calls != 2 {
		t.Fatalf("live hook calls = %d", calls)
	}
}
