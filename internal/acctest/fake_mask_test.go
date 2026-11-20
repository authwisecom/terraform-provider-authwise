package acctest_test

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// refuseBadMasks is kit's Patch rule (kit's release after 1.96.0): a Patch
// with no update mask, or an empty one, is INVALID_ARGUMENT, and so is a
// path that is not a proto field path of the patched entity.
func refuseBadMasks() grpc.ServerOption {
	return grpc.ChainUnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		method := info.FullMethod[strings.LastIndex(info.FullMethod, "/")+1:]
		m, ok := req.(proto.Message)
		if !ok || !strings.HasPrefix(method, "Patch") {
			return handler(ctx, req)
		}
		if err := checkPatchMask(m.ProtoReflect()); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "%s: %v", method, err)
		}
		return handler(ctx, req)
	})
}

func checkPatchMask(r protoreflect.Message) error {
	fields := r.Descriptor().Fields()
	maskField := fields.ByName("update_mask")
	if maskField == nil || !r.Has(maskField) {
		return status.Error(codes.InvalidArgument, "update_mask is required")
	}
	mask, ok := r.Get(maskField).Message().Interface().(*fieldmaskpb.FieldMask)
	if !ok || len(mask.GetPaths()) == 0 {
		return status.Error(codes.InvalidArgument, "update_mask is empty")
	}
	for i := range fields.Len() {
		f := fields.Get(i)
		if f.Kind() != protoreflect.MessageKind || f.Name() == "update_mask" {
			continue
		}
		entity := r.Get(f).Message().Interface()
		if !mask.IsValid(entity) {
			return status.Errorf(codes.InvalidArgument, "update_mask %v names a path that is not a field of %s", mask.GetPaths(), f.Message().FullName())
		}
	}
	return nil
}
