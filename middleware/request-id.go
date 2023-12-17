package middleware

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"one-api/common"
)

func RequestId() func(c *gin.Context) {
	return func(c *gin.Context) {
		tracer := otel.Tracer("one-api/controller/request-id")
		ctx, span := tracer.Start(c.Request.Context(), "RequestId")
		defer span.End()
		id := uuid.NewString()
		c.Set(common.RequestIdKey, id)
		ctx = context.WithValue(ctx, common.RequestIdKey, id)
		c.Request = c.Request.WithContext(ctx)
		c.Header(common.RequestIdKey, id)
		c.Next()
	}
}
