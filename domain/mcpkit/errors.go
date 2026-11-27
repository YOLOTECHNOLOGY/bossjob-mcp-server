package mcpkit

import (
	"context"
	"errors"
	"fmt"

	"github.com/YOLOTECHNOLOGY/bossjob-lib-go/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func InvalidArgument(format string, args ...any) error {
	return status.Error(codes.InvalidArgument, fmt.Sprintf(format, args...))
}

// Validatable 为 protoc-gen-validate 生成的校验方法。
type Validatable interface{ ValidateAll() error }

func Validate(req Validatable) error {
	if err := req.ValidateAll(); err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	return nil
}

// UpstreamError 将下游 gRPC 错误转为面向模型的错误：业务可理解的状态码保留原消息，
// 内部错误只返回通用描述，原始错误写日志。
func UpstreamError(ctx context.Context, op string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return status.Error(codes.DeadlineExceeded, op+" timed out, please retry")
	}
	st, ok := status.FromError(err)
	if !ok {
		log.ErrorfCtx(ctx, "mcp upstream %s failed: %v", op, err)
		return status.Error(codes.Unavailable, op+" is temporarily unavailable")
	}
	switch st.Code() {
	case codes.InvalidArgument, codes.NotFound, codes.AlreadyExists, codes.FailedPrecondition,
		codes.PermissionDenied, codes.Unauthenticated, codes.ResourceExhausted, codes.OutOfRange:
		return st.Err()
	default:
		log.ErrorfCtx(ctx, "mcp upstream %s failed: code=%s msg=%s", op, st.Code(), st.Message())
		return status.Error(codes.Unavailable, op+" is temporarily unavailable")
	}
}
