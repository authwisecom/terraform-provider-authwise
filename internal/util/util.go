package util

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

var protoUnmarshaller = protojson.UnmarshalOptions{
	DiscardUnknown: true,
}

func JsonToProtoAny(j jsontypes.Normalized) (*anypb.Any, error) {
	res := &anypb.Any{}
	converted, err := jsonToMap(j)
	if err != nil {
		return res, err
	}
	bytes, err := json.Marshal(converted)
	if err != nil {
		return nil, err
	}
	bytesValue := &wrapperspb.BytesValue{Value: bytes}
	err = anypb.MarshalFrom(res, bytesValue, proto.MarshalOptions{})
	if err != nil {
		return nil, err
	}
	return res, nil
}
func JsonToProtoStruct(j jsontypes.Normalized) (*structpb.Struct, error) {
	converted, err := jsonToMap(j)
	if err != nil {
		return nil, err
	}
	res, err := structpb.NewStruct(converted)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func jsonToMap(j jsontypes.Normalized) (map[string]any, error) {
	res := map[string]any{}
	if j.ValueString() == "" || j.IsNull() {
		return res, nil
	}
	diag := j.Unmarshal(&res)
	if diag.HasError() {
		return res, errors.New("unable to unmarshal json string to map")
	}
	return res, nil
}
func ProtoStructToJson(s *structpb.Struct) (*jsontypes.Normalized, error) {
	return mapToJson(s.AsMap())
}

func ProtoAnyToJson(a *anypb.Any) (*jsontypes.Normalized, error) {
	var val map[string]any

	bytesValue := &wrapperspb.BytesValue{}
	err := anypb.UnmarshalTo(a, bytesValue, proto.UnmarshalOptions{})
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(bytesValue.Value, &val)
	if err != nil {
		return nil, err
	}

	return mapToJson(val)
}
func JsonToProtoConcrete[T any](j jsontypes.Normalized) (*T, error) {
	converted, err := jsonToMap(j)
	if err != nil {
		return nil, err
	}
	var res any
	val := new(T)
	res = val

	jsonString, err := json.Marshal(converted)

	if err != nil {
		return nil, err
	}
	if msg, ok := res.(proto.Message); ok {
		err = protoUnmarshaller.Unmarshal(jsonString, msg)
		if err != nil {
			return nil, err
		}

		return val, nil
	} else {
		return nil, errors.New("must be proto.Message")
	}
}
func mapToJson(m map[string]any) (*jsontypes.Normalized, error) {
	jsonString, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	val := jsontypes.NewNormalizedValue(string(jsonString))
	return &val, nil
}

// TODO - is there a better way
func ListToSlice[T any](l types.List) ([]T, error) {
	res := make([]T, 0, len(l.Elements()))
	diag := l.ElementsAs(context.Background(), &res, false)
	if diag.HasError() {
		return []T{}, errors.New("can not convert types.list to []T")
	}
	return res, nil
}

/*
	protoreflect.StringKind: "StringType",
	protoreflect.BytesKind:  "StringType",
	protoreflect.Int32Kind:  "Int64Type",
	protoreflect.Int64Kind:  "Int64Type",
	protoreflect.EnumKind:   "Int64Type",
	protoreflect.FloatKind:  "Float64Type",
	protoreflect.DoubleKind: "Float64Type",
	protoreflect.BoolKind:   "BoolType",
*/

// converts tf list type to underlying primitive slice
// removes nil values
func tfListToPrimitiveSlice[T any](ctx context.Context, v attr.Value) ([]T, error) {
	list := v.(types.List)
	if list.Elements() == nil {
		return nil, nil
	}

	elements := make([]T, 0, len(list.Elements()))
	diag := list.ElementsAs(ctx, &elements, true)
	if diag.HasError() {
		return elements, errors.New("can not convert types.list to underlying type slice")
	}
	return elements, nil
}

// TODO - add cases for each list type
func ObjectToMap(obj types.Object) (map[string]any, error) {
	ctx := context.Background()
	attrs := obj.Attributes()
	res := map[string]any{}

	for k, v := range attrs {
		switch v.Type(ctx) {
		case types.StringType:
			res[k] = v.(types.String).ValueString()
		case types.Int64Type:
			res[k] = v.(types.Int64).ValueInt64()
		case types.Float64Type:
			res[k] = v.(types.Float64).ValueFloat64()
		case types.BoolType:
			res[k] = v.(types.Bool).ValueBool()
		case types.ListType{
			ElemType: types.StringType,
		}:
			elements, err := tfListToPrimitiveSlice[string](ctx, v)
			if err != nil {
				return res, err
			}
			if elements != nil {
				res[k] = elements
			}
		case types.ListType{
			ElemType: types.Int64Type,
		}:
			elements, err := tfListToPrimitiveSlice[int](ctx, v)
			if err != nil {
				return res, err
			}
			if elements != nil {
				res[k] = elements
			}
		case types.ListType{
			ElemType: types.BoolType,
		}:
			elements, err := tfListToPrimitiveSlice[bool](ctx, v)
			if err != nil {
				return res, err
			}
			if elements != nil {
				res[k] = elements
			}
		case types.ListType{
			ElemType: types.Float64Type,
		}:
			elements, err := tfListToPrimitiveSlice[float64](ctx, v)
			if err != nil {
				return res, err
			}
			if elements != nil {
				res[k] = elements
			}
		default:
			//TODO - maps
			//handle objects
			ret, err := ObjectToMap(v.(types.Object))
			if err != nil {
				return nil, err
			}
			res[k] = ret
		}
	}

	return res, nil
}
