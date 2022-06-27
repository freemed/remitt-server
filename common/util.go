package common

import (
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
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

func JsonEncode(o any) []byte {
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

func ParamInt(c *gin.Context, param string) (int64, error) {
	p := c.Param(param)
	if p == "" {
		return int64(0), errors.New("bad parameter")
	}
	return strconv.ParseInt(p, 10, 64)
}

func ParamMustInt(c *gin.Context, param string) int64 {
	p := c.Param(param)
	if p == "" {
		return int64(0)
	}
	i, _ := strconv.ParseInt(p, 10, 64)
	return i
}
