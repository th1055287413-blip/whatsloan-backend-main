package logger

import (
	"context"

	"go.uber.org/zap"
)

type ctxKey struct{}

// Ctx 從 context 取得 logger，fallback 到全域 BaseLogger
func Ctx(ctx context.Context) *zap.SugaredLogger {
	if ctx == nil {
		return BaseLogger
	}
	if l, ok := ctx.Value(ctxKey{}).(*zap.SugaredLogger); ok {
		return l
	}
	return BaseLogger
}

// WithCtx 將帶有額外欄位的 logger 存入 context
func WithCtx(ctx context.Context, args ...interface{}) context.Context {
	l := Ctx(ctx).With(args...)
	return context.WithValue(ctx, ctxKey{}, l)
}

// WithConnectorCtx 注入 connector_id 到 context
func WithConnectorCtx(ctx context.Context, connectorID string) context.Context {
	return WithCtx(ctx, "connector_id", connectorID)
}

// WithAccountCtx 注入 account_id 到 context
func WithAccountCtx(ctx context.Context, accountID uint) context.Context {
	return WithCtx(ctx, "account_id", accountID)
}

// WithPhoneCtx 注入 phone 到 context
func WithPhoneCtx(ctx context.Context, phone string) context.Context {
	return WithCtx(ctx, "phone", phone)
}

// WithRequestCtx 注入 request_id 到 context
func WithRequestCtx(ctx context.Context, requestID string) context.Context {
	return WithCtx(ctx, "request_id", requestID)
}

// WithUserCtx 注入 user_id 到 context
func WithUserCtx(ctx context.Context, userID uint) context.Context {
	return WithCtx(ctx, "user_id", userID)
}

// WithEventCtx 注入 connector_id + account_id 到 context
func WithEventCtx(ctx context.Context, connectorID string, accountID uint) context.Context {
	return WithCtx(ctx, "connector_id", connectorID, "account_id", accountID)
}
