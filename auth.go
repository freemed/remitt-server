package main

import (
	"encoding/base64"
	"github.com/gin-gonic/gin"
	"log"
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
			log.Printf("BasicAuth(): found %v", auth)
			c.Header("WWW-Authenticate", realm)
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		payload, _ := base64.StdEncoding.DecodeString(auth[1])
		pair := strings.SplitN(string(payload), ":", 2)
		if !afunc(pair[0], pair[1]) {
			// Credentials doesn't match, we return 401 and abort handlers chain.
			log.Printf("BasicAuth(): Credentials for %s do not match", pair[0])
			c.Header("WWW-Authenticate", realm)
			c.AbortWithStatus(401)
		} else {
			// The user credentials was found, set user's id to key AuthUserKey in this context, the userId can be read later using
			// c.MustGet(gin.AuthUserKey)
			log.Printf("BasicAuth() [%s]: Authenticated user", pair[0])
			c.Set(gin.AuthUserKey, pair[0])
		}
	}
}
