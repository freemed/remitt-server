package transport

// script_mail_test.go pins the JS mail client contract (script_mail.go) which a
// Script transport exposes to plugin scripts as `mail` (script.go:Initialize).
//
// The identity used for the recipient address comes from the DATABASE (the
// tUser row for o.obj.user.Username, via model.GetUserByName) while the SMTP
// server and From address come from config.Config.Mail - both sources are
// asserted here. The SMTP endpoint used by the tests is an in-process test
// double listening on loopback (it speaks just enough SMTP to accept one
// message); no external SMTP server is contacted. Sending through a real mail
// server is skipped with an explicit reason.

import (
	"bufio"
	"database/sql/driver"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/freemed/remitt-server/config"
)

// ---------------------------------------------------------------------------
// Loopback SMTP test double
// ---------------------------------------------------------------------------

// fakeSMTP listens on loopback, accepts one SMTP session, captures the DATA
// payload and returns it on the channel.
func fakeSMTP(t *testing.T) (port int, message <-chan string) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for fake SMTP: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address %v is not a *net.TCPAddr", l.Addr())
	}

	ch := make(chan string, 1)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			ch <- ""
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
		r := bufio.NewReader(conn)
		w := bufio.NewWriter(conn)
		say := func(s string) {
			_, _ = w.WriteString(s + "\r\n")
			_ = w.Flush()
		}
		say("220 transport-test ESMTP")

		var body strings.Builder
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				ch <- body.String()
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if inData {
				if line == "." {
					inData = false
					say("250 2.0.0 Ok: queued")
					continue
				}
				body.WriteString(line + "\n")
				continue
			}
			verb := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(verb, "EHLO"), strings.HasPrefix(verb, "HELO"):
				say("250-transport-test")
				say("250 8BITMIME")
			case strings.HasPrefix(verb, "DATA"):
				inData = true
				say("354 End data with <CR><LF>.<CR><LF>")
			case strings.HasPrefix(verb, "QUIT"):
				say("221 2.0.0 Bye")
				ch <- body.String()
				return
			case verb == "":
				// ignore
			default:
				say("250 2.0.0 Ok")
			}
		}
	}()
	return addr.Port, ch
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func TestScriptMail_SendMessage_DeliversToUserEmailAddress(t *testing.T) {
	port, message := fakeSMTP(t)

	prev := config.Config
	config.Config = &config.AppConfig{}
	config.Config.Mail.Server = "127.0.0.1"
	config.Config.Mail.Port = port
	config.Config.Mail.TLS = false
	config.Config.Mail.FromAddress = "remitt@example.org"
	t.Cleanup(func() { config.Config = prev })

	withStubQueries(t, nil, userRowColumns, [][]driver.Value{userRow(1, "admin", "admin@example.org")})

	ic := testInterpreter()
	m := &mail{obj: ic}

	if ok := m.SendMessage("Claim accepted", "text/plain", "Your claim was accepted."); !ok {
		t.Fatal("SendMessage() = false; want true against the loopback SMTP test double")
	}

	select {
	case got := <-message:
		if !strings.Contains(got, "To:") || !strings.Contains(got, "admin@example.org") {
			t.Errorf("message is missing the recipient from the database (tUser.contactEmail):\n%s", got)
		}
		if !strings.Contains(got, "From:") || !strings.Contains(got, "remitt@example.org") {
			t.Errorf("message is missing the From address from config.Config.Mail.FromAddress:\n%s", got)
		}
		if !strings.Contains(got, "Subject: Claim accepted") {
			t.Errorf("message is missing the subject:\n%s", got)
		}
		if !strings.Contains(got, "Content-Type: text/plain") {
			t.Errorf("message is missing the requested content type:\n%s", got)
		}
		if !strings.Contains(got, "Your claim was accepted.") {
			t.Errorf("message body is missing:\n%s", got)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("no SMTP message reached the loopback test double within 20s")
	}
}

func TestScriptMail_SendMessage_UnknownUserReturnsFalse(t *testing.T) {
	prev := config.Config
	config.Config = &config.AppConfig{}
	config.Config.Mail.Server = "127.0.0.1"
	config.Config.Mail.Port = 1
	t.Cleanup(func() { config.Config = prev })

	// No rows: model.GetUserByName fails, so no mail is attempted.
	withStubQueries(t, nil, userRowColumns, nil)

	ic := testInterpreter()
	m := &mail{obj: ic}
	if ok := m.SendMessage("subject", "text/plain", "body"); ok {
		t.Fatal("SendMessage() = true for a user that does not exist in tUser; want false")
	}
}

func TestScriptMail_SendMessage_DatabaseErrorReturnsFalse(t *testing.T) {
	prev := config.Config
	config.Config = &config.AppConfig{}
	config.Config.Mail.Server = "127.0.0.1"
	config.Config.Mail.Port = 1
	t.Cleanup(func() { config.Config = prev })

	withStubQueries(t, &stubDB{queryErr: errStubQuery}, nil, nil)

	ic := testInterpreter()
	m := &mail{obj: ic}
	if ok := m.SendMessage("subject", "text/plain", "body"); ok {
		t.Fatal("SendMessage() = true while the user lookup failed; want false")
	}
}

// TestScriptMail_SendMessage_UnreachableSMTPServerReturnsFalse checks the
// plugin reports failure (rather than panicking or reporting success) when
// config.Config.Mail points at a port nothing is listening on.
func TestScriptMail_SendMessage_UnreachableSMTPServerReturnsFalse(t *testing.T) {
	prev := config.Config
	config.Config = &config.AppConfig{}
	config.Config.Mail.Server = "127.0.0.1"
	config.Config.Mail.Port = closedLocalPort(t)
	config.Config.Mail.FromAddress = "remitt@example.org"
	t.Cleanup(func() { config.Config = prev })

	withStubQueries(t, nil, userRowColumns, [][]driver.Value{userRow(1, "admin", "admin@example.org")})

	ic := testInterpreter()
	m := &mail{obj: ic}
	if ok := m.SendMessage("subject", "text/plain", "body"); ok {
		t.Fatal("SendMessage() = true with nothing listening on the configured SMTP port; want false")
	}
}

// TestScriptMail_SendMessage_MissingRecipientReturnsFalse pins that the
// recipient address is taken from tUser.contactEmail: a user with no contact
// email cannot be mailed and the helper reports failure instead of sending to
// an empty address.
func TestScriptMail_SendMessage_MissingRecipientReturnsFalse(t *testing.T) {
	port, _ := fakeSMTP(t)

	prev := config.Config
	config.Config = &config.AppConfig{}
	config.Config.Mail.Server = "127.0.0.1"
	config.Config.Mail.Port = port
	config.Config.Mail.FromAddress = "remitt@example.org"
	t.Cleanup(func() { config.Config = prev })

	// contactemail is NULL for this user.
	withStubQueries(t, nil, userRowColumns, [][]driver.Value{{
		1, "admin", "$2a$10$notarealhash", nil, nil, nil, nil, nil, "Administrator",
	}})

	ic := testInterpreter()
	m := &mail{obj: ic}
	if ok := m.SendMessage("subject", "text/plain", "body"); ok {
		t.Fatal("SendMessage() = true for a user with no contactEmail; want false")
	}
}

// TestScriptMail_SendMessage_NilConfigReturnsFalse pins the corrected
// behaviour of the defect this test used to document
// (TestScriptMail_SendMessage_NilConfigPanics): common.NewMailer()
// dereferences config.Config unconditionally (common/mail.go:16-20), so a
// process whose configuration was never loaded panicked inside the job worker
// when a plugin script called mail.SendMessage(). The script now receives a
// failure result - the helper returns false and nothing unwinds into the
// worker.
func TestScriptMail_SendMessage_NilConfigReturnsFalse(t *testing.T) {
	prev := config.Config
	config.Config = nil
	t.Cleanup(func() { config.Config = prev })

	withStubQueries(t, nil, userRowColumns, [][]driver.Value{userRow(1, "admin", "admin@example.org")})

	ic := testInterpreter()
	m := &mail{obj: ic}

	// A panic must fail this test rather than pass as "returned a value".
	defer func() {
		if caught := recover(); caught != nil {
			t.Fatalf("SendMessage() with config.Config == nil panicked with %v; a script must receive a failure result, never a panic", caught)
		}
	}()
	if ok := m.SendMessage("subject", "text/plain", "body"); ok {
		t.Fatal("SendMessage() = true with config.Config == nil; want false")
	}

	// The same call made the way a plugin script makes it: otto must hand the
	// failure value back to the script instead of a panic escaping the VM and
	// taking the job worker with it.
	got, err := evalJS(t, ic, `result = mail.SendMessage("subject", "text/plain", "body");`, "result")
	if err != nil {
		t.Fatalf("mail.SendMessage() from JS with config.Config == nil: %v", err)
	}
	if got != "false" {
		t.Fatalf("mail.SendMessage() from JS returned %s with config.Config == nil; want false", got)
	}
}

// TestScriptMail_SendMessage_IsCallableFromAPluginScript drives the helper the
// way a Script transport does (through otto) and pins the JS entry point name
// scripts must use: mail.SendMessage, not mail.sendMessage (otto exposes Go
// method names verbatim, so a lowerCamelCase call is a TypeError).
func TestScriptMail_SendMessage_IsCallableFromAPluginScript(t *testing.T) {
	port, message := fakeSMTP(t)

	prev := config.Config
	config.Config = &config.AppConfig{}
	config.Config.Mail.Server = "127.0.0.1"
	config.Config.Mail.Port = port
	config.Config.Mail.FromAddress = "remitt@example.org"
	t.Cleanup(func() { config.Config = prev })

	withStubQueries(t, nil, userRowColumns, [][]driver.Value{userRow(1, "admin", "admin@example.org")})

	ic := testInterpreter()

	if _, err := ic.vm.Run(`result = mail.sendMessage("s", "text/plain", "b");`); err == nil {
		t.Fatal("mail.sendMessage() from JS succeeded; otto now lowercases the method name - update this test")
	} else if !strings.Contains(err.Error(), "is not a function") {
		t.Fatalf("mail.sendMessage() from JS failed with %v; want a TypeError", err)
	}

	if _, err := ic.vm.Run(`result = mail.SendMessage("Claim accepted", "text/plain", "Your claim was accepted.");`); err != nil {
		t.Fatalf("mail.SendMessage() from JS: %v", err)
	}
	v, err := ic.vm.Get("result")
	if err != nil {
		t.Fatal(err)
	}
	if got := v.String(); got != "true" {
		t.Fatalf("mail.SendMessage() returned %s; want true", got)
	}

	select {
	case got := <-message:
		if !strings.Contains(got, "admin@example.org") || !strings.Contains(got, "Subject: Claim accepted") {
			t.Errorf("message captured from the scripted call is not the expected one:\n%s", got)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("no SMTP message reached the loopback test double within 20s")
	}
}

func TestScriptMail_RealSMTPServerRequired(t *testing.T) {
	t.Skip("requires a live SMTP server: verifying delivery, STARTTLS negotiation and authentication against a real mail server cannot be done in-process; the local test double only proves the message the plugin builds")
}
