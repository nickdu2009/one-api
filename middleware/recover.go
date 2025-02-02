package middleware

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"net/http"
	"one-api/common"
)

func RelayPanicRecover() gin.HandlerFunc {
	return func(c *gin.Context) {

		tracer := otel.Tracer("one-api/middleware/recover")
		_, span := tracer.Start(c.Request.Context(), "RelayPanicRecover")
		defer span.End()

		defer func() {
			if err := recover(); err != nil {
				common.SysError(fmt.Sprintf("panic detected: %v", err))
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": gin.H{
						"message": fmt.Sprintf("Panic detected, error: %v. Please submit a issue here: https://github.com/songquanpeng/one-api", err),
						"type":    "one_api_panic",
					},
				})
				c.Abort()
			}
		}()
		c.Next()
	}
}
