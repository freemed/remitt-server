package transport

import (
	"log"

	"github.com/freemed/remitt-server/common"
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
	d := common.NewMailer()
	err = d.SendMessage(userObj.Username, userObj.ContactEmail.String, subject, contentType, text)
	if err != nil {
		log.Printf("JS.mail.SendMessage: %s", err.Error())
	}
	return err == nil
}
