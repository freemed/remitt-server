package transport

import (
	"context"
	"testing"

	"github.com/freemed/remitt-server/model"
	"github.com/freemed/remitt-server/model/user"
)

func Test_Script(t *testing.T) {
	u := model.UserModel{
		Username: "admin",
		Id:       1,
	}
	s := Script{ctx: user.NewContext(context.Background(), &u)}
	s.SetOptions(map[string]interface{}{
		"script": `
		log('test -- data = ' + String.fromCharCode.apply(String, data));
		`,
		"timeout": 1,
	})
	err := s.Transport("testfile.txt", ([]byte)("ABCDEFG"))
	if err != nil {
		t.Fatalf(err.Error())
	}
}
