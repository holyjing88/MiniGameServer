package logging

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

var protoJSON = protojson.MarshalOptions{
	EmitUnpopulated: true,
	UseProtoNames:   true,
}

// UnaryServerInterceptor logs gRPC API calls with request/response payloads.
func UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()
		reqID := ""
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if vals := md.Get("x-request-id"); len(vals) > 0 && vals[0] != "" {
				reqID = vals[0]
			}
		}
		if reqID == "" {
			reqID = NewRequestID()
		}
		ctx = WithRequestID(ctx, reqID)

		Trace("grpc.api.start req_id=%s method=%s", reqID, info.FullMethod)
		Info("grpc.api.req req_id=%s method=%s has_auth=%v params=%s",
			reqID, info.FullMethod, grpcHasAuth(ctx), formatProtoPayload(req))

		resp, err := handler(ctx, req)
		dur := time.Since(start)
		st, _ := status.FromError(err)
		code := codes.OK
		if st != nil {
			code = st.Code()
		}
		msg := "grpc.api.done req_id=%s method=%s code=%s dur_ms=%.2f"
		switch {
		case err != nil && (code == codes.Internal || code == codes.Unknown || code == codes.Unavailable):
			Error(msg+" err=%v", reqID, info.FullMethod, code, float64(dur.Microseconds())/1000, err)
		case err != nil:
			Info(msg+" err=%v", reqID, info.FullMethod, code, float64(dur.Microseconds())/1000, err)
		default:
			Info(msg, reqID, info.FullMethod, code, float64(dur.Microseconds())/1000)
		}
		if err == nil {
			Info("grpc.api.resp req_id=%s method=%s result=%s",
				reqID, info.FullMethod, formatProtoPayload(resp))
		} else if st != nil {
			Info("grpc.api.resp req_id=%s method=%s result=err code=%s msg=%q",
				reqID, info.FullMethod, st.Code(), st.Message())
		}
		return resp, err
	}
}

func formatProtoPayload(v interface{}) string {
	if v == nil {
		return ""
	}
	msg, ok := v.(proto.Message)
	if !ok {
		return truncateRunes(strings.TrimSpace(fmt.Sprintf("%T", v)), maxLogPayload)
	}
	b, err := protoJSON.Marshal(msg)
	if err != nil {
		return "marshal_error=" + err.Error()
	}
	return FormatPayload(b)
}

func grpcHasAuth(ctx context.Context) bool {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return false
	}
	if len(md.Get("authorization")) > 0 {
		return true
	}
	if len(md.Get("x-rank-service-token")) > 0 {
		return true
	}
	return false
}
