package common

import (
	"github.com/labstack/echo/v5"
)

const AuthUserKey = "user"

var (
	ApiMap = map[string]ApiMapping{}
)

type ApiMapping func(*echo.Group)
