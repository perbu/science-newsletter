package email

import (
	"context"
	"fmt"

	"github.com/resend/resend-go/v2"
)

// Mailer sends emails via the Resend API.
type Mailer struct {
	client *resend.Client
	from   string
}

// NewMailer creates a Mailer with the given API key and default sender address.
func NewMailer(apiKey, from string) *Mailer {
	return &Mailer{
		client: resend.NewClient(apiKey),
		from:   from,
	}
}

// Send delivers an HTML email to the given recipients.
func (m *Mailer) Send(ctx context.Context, to []string, subject, html string) error {
	_, err := m.client.Emails.SendWithContext(ctx, &resend.SendEmailRequest{
		From:    m.from,
		To:      to,
		Subject: subject,
		Html:    html,
	})
	if err != nil {
		return fmt.Errorf("resend send: %w", err)
	}
	return nil
}
