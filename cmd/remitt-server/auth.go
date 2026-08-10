package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/freemed/remitt-server/model"
	"github.com/labstack/echo/v5"
)

const AuthUserKey = "user"

// LoadUserMiddleware loads the full UserModel and roles from the database
// after BasicAuth has validated credentials. Must be used after
// middleware.BasicAuth in the middleware chain.
func LoadUserMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			// Echo v5 BasicAuth middleware validates credentials but does not
			// store the username in context. We extract it from the request
			// Authorization header directly.
			username, _, _ := c.Request().BasicAuth()
			if username == "" {
				return echo.NewHTTPError(http.StatusInternalServerError, "auth: no valid basic auth credentials")
			}

			c.Set(AuthUserKey, username)

			u, err := model.GetUserByName(username)
			if err != nil {
				log.Printf("LoadUserMiddleware(): GetUserByName: %s", err.Error())
				return echo.NewHTTPError(http.StatusInternalServerError,
					fmt.Sprintf("auth: getuserbyname: %s", err.Error()))
			}
			c.Set("userObj", u)

			r, err := u.GetRoles()
			if err == nil {
				c.Set("roles", r)
			} else {
				c.Set("roles", []string{})
			}

			return next(c)
		}
	}
}
