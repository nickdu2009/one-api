package middleware

import (
	"context"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"one-api/common"
)

func RequestId() func(c *gin.Context) {
	return func(c *gin.Context) {
		tracer := otel.Tracer("one-api/controller/request-id")
		ctx, span := tracer.Start(c.Request.Context(), "RequestId")
		defer span.End()
		id := common.GetTimeString() + common.GetRandomString(8)
		c.Set(common.RequestIdKey, id)
		ctx := context.WithValue(c.Request.Context(), common.RequestIdKey, id)
		c.Request = c.Request.WithContext(ctx)
		c.Header(common.RequestIdKey, id)
		c.Next()
	}
}
