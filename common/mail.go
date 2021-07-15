package common

import (
	"github.com/freemed/remitt-server/config"
	gomail "gopkg.in/gomail.v2"
)

// Mailer encapsulates a mail sending mechanism
type Mailer struct {
	dialer *gomail.Dialer
}

// NewMailer creates a new instance of the Mailer sending object
func NewMailer() Mailer {
	m := Mailer{}
	m.dialer = &gomail.Dialer{
		Host: config.Config.Mail.Server,
		Port: config.Config.Mail.Port,
		SSL:  config.Config.Mail.TLS,
	}
	return m
}

// SendMessage sends a notification message (with error handling)
func (m Mailer) SendMessage(toName, toEmail, subject, contentType, text string) error {
	s, err := m.dialer.Dial()
	if err != nil {
		return err
	}
	defer s.Close() // close connection to SMTP server when we're finished

	msg := gomail.NewMessage()
	msg.SetHeader("From", config.Config.Mail.FromAddress)
	msg.SetAddressHeader("To", toEmail, toName)
	msg.SetHeader("Subject", subject)
	if contentType == "" {
		msg.SetBody("text/plain", text)
	} else {
		msg.SetBody(contentType, text)
	}

	return gomail.Send(s, msg)
}
