package main

import (
	"encoding/base64"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"strings"
)

// BasicAuth is a variation of the gin.BasicAuth middleware allowing for a
// callback function, rather than preloading credentials.
func BasicAuth(afunc func(string, string) bool, realm string) gin.HandlerFunc {
	if realm == "" {
		realm = "Authorization Required"
	}
	realm = "Basic realm=" + strconv.Quote(realm)
	return func(c *gin.Context) {
		// Search user in the slice of allowed credentials
		auth := strings.SplitN(c.Request.Header.Get("Authorization"), " ", 2)

		if len(auth) != 2 || auth[0] != "Basic" {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}

		payload, _ := base64.StdEncoding.DecodeString(auth[1])
		pair := strings.SplitN(string(payload), ":", 2)
		if !afunc(pair[0], pair[1]) {
			// Credentials doesn't match, we return 401 and abort handlers chain.
			c.Header("WWW-Authenticate", realm)
			c.AbortWithStatus(401)
		} else {
			// The user credentials was found, set user's id to key AuthUserKey in this context, the userId can be read later using
			// c.MustGet(gin.AuthUserKey)
			c.Set(gin.AuthUserKey, pair[0])
		}
	}
}
