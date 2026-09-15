package transport

import (
	"log"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/config"
	"github.com/freemed/remitt-server/model"
)

type mail struct {
	obj *Interpreter
}

func (o *mail) SendMessage(subject, contentType, text string) bool {
	log.Printf("JS.mail.SendMessage: %s/%s/%s", o.obj.user.Username, subject, contentType)
	userObj, err := model.GetUserByName(o.obj.user.Username)
	if err != nil {
		log.Printf("JS.mail.SendMessage: %s", err.Error())
		return false
	}
	// The SMTP server and the From address come from config.Config, which
	// common.NewMailer() dereferences unconditionally (common/mail.go:16-20). A
	// process whose configuration was never loaded (or failed to load) must
	// fail this call, not panic: a panic here unwinds through otto's VM into
	// the job worker, taking the worker down with it. The guard sits directly
	// on the use of config.Config below, like the database guard in
	// storefile.go.
	if config.Config == nil {
		log.Printf("JS.mail.SendMessage: %s: configuration is not loaded", o.obj.user.Username)
		return false
	}
	d := common.NewMailer()
	err = d.SendMessage(userObj.Username, userObj.ContactEmail.String, subject, contentType, text)
	if err != nil {
		log.Printf("JS.mail.SendMessage: %s", err.Error())
	}
	return err == nil
}
