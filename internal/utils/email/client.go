package email

import (
	"context"
	"fmt"
	"time"

	"monitor-central/internal/config"
	"monitor-central/internal/models"

	"github.com/wneessen/go-mail"
)

// Client handles generic email sending
type Client struct {
	host          string
	clientOptions []mail.Option
	from          string
}

// NewClient creates a new email client
func NewClient(cfg *config.SMTPConfig) *Client {
	options := []mail.Option{
		mail.WithPort(cfg.Port),
		mail.WithTimeout(30 * time.Second),
	}

	if cfg.User != "" && cfg.Pass != "" {
		options = append(
			options,
			mail.WithSMTPAuth(mail.SMTPAuthPlain),
			mail.WithUsername(cfg.User),
			mail.WithPassword(cfg.Pass),
		)
	}

	if cfg.Host == "localhost" ||
		cfg.Host == "127.0.0.1" ||
		cfg.Host == "mailpit" {
		options = append(
			options,
			mail.WithTLSPolicy(mail.NoTLS),
		)
	} else {
		options = append(
			options,
			mail.WithTLSPolicy(mail.TLSMandatory),
		)
	}

	return &Client{
		host:          cfg.Host,
		clientOptions: options,
		from:          cfg.From,
	}
}

// SendHTML sends an HTML email
func (c *Client) SendNotification(ctx context.Context, n models.EmailNotification) error {
	// Create a new client for each send
	client, err := mail.NewClient(c.host, c.clientOptions...)
	if err != nil {
		return fmt.Errorf("failed to create mail client: %w", err)
	}
	defer client.Close()

	// Create a new message
	msg := mail.NewMsg()
	if err := msg.From(c.from); err != nil {
		return fmt.Errorf("failed to set From address: %w", err)
	}
	if err := msg.To(n.Recipient); err != nil {
		return fmt.Errorf("failed to set To address: %w", err)
	}
	msg.Subject(n.Subject)
	msg.SetMessageIDWithValue(fmt.Sprintf("monitor-outbox-%d@monitor.local", n.ID))

	// Set plain text as fallback and HTML as alternative
	msg.SetBodyString(mail.TypeTextPlain, n.TextBody)
	msg.AddAlternativeString(mail.TypeTextHTML, n.HTMLBody)

	// Open connection, send the email, and close connection
	if err := client.DialAndSendWithContext(ctx, msg); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}
