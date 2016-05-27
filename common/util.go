package common

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"io/ioutil"
	"log"
	"time"
)

var (
	IsRunning = true
)

func Md5hash(orig string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(orig)))
}

func SleepFor(sec int64) {
	for i := 0; i < int(sec); i++ {
		if !IsRunning {
			return
		}
		time.Sleep(time.Second)
	}
}

func JsonEncode(o interface{}) []byte {
	b, err := json.Marshal(o)
	if err != nil {
		log.Print(err.Error())
		return []byte("false")
	}
	return b
}

func BodyFromContext(c *gin.Context) ([]byte, error) {
	defer c.Request.Body.Close()
	return ioutil.ReadAll(c.Request.Body)
}
