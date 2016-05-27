package common

import (
	"github.com/gin-gonic/gin"
)

var (
	ApiMap = map[string]ApiMapping{}
)

type ApiMapping func(*gin.RouterGroup)
