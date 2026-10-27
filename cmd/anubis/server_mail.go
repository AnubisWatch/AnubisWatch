package main

import (
	"fmt"
	"log/slog"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/mail"
	"github.com/AnubisWatch/anubiswatch/internal/statuspage"
)

// newMailQueue builds the outgoing mail queue from the mail: section, or
// returns nil when no relay is configured.
func newMailQueue(cfg *core.Config, logger *slog.Logger) (*mail.Queue, error) {
	if !cfg.Mail.Enabled() {
		return nil, nil
	}
	mailer, err := mail.New(cfg.Mail)
	if err != nil {
		return nil, fmt.Errorf("mail: %w", err)
	}
	logger.Info("outgoing mail enabled",
		"host", cfg.Mail.Host,
		"port", cfg.Mail.EffectivePort(),
		"tls", cfg.Mail.EffectiveTLS(),
		"public_url", cfg.Server.PublicBaseURL())
	return mail.NewQueue(mailer, logger, mail.QueueOptions{}), nil
}

// incidentHook feeds alert incidents to the live WebSocket stream and, when
// subscriptions are enabled, to status page subscribers.
func incidentHook(live func(*core.Incident), pages *statuspage.Handler) func(*core.Incident) {
	if pages == nil || !pages.SubscriptionsAvailable() {
		return live
	}
	return func(incident *core.Incident) {
		live(incident)
		pages.NotifyIncident(incident)
	}
}
