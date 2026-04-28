package router

import (
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
)

func SetGeneratedImageRouter(router *gin.Engine) {
	dir := common.GetGeneratedImageDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		common.SysError("failed to create generated image dir: " + err.Error())
		return
	}
	route := strings.TrimSuffix(common.GeneratedImageRoutePrefix, "/")
	router.Static(route, dir)
}
