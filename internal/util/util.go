package util

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"reflect"
)

var protoUnmarshaller = protojson.UnmarshalOptions{
	DiscardUnknown: true,
}

func ObjectToProtoAny(obj types.Object) (*anypb.Any, error) {
	res := &anypb.Any{}
	converted, err := objectToMap(obj)
	if err != nil {
		return nil, err
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

func ObjectToProtoStruct(obj types.Object) (*structpb.Struct, error) {
	converted, err := objectToMap(obj)
	if err != nil {
		return nil, err
	}
	res, err := structpb.NewStruct(converted)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func ObjectToProtoConcrete[T any](obj types.Object) (*T, error) {
	converted, err := objectToMap(obj)
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

func objectToMap(obj types.Object) (map[string]any, error) {
	attrs := obj.Attributes()
	res := map[string]any{}

	for k, v := range attrs {
		switch v.Type(context.Background()) {
		case types.StringType:
			res[k] = v.(types.String).ValueString()
		case types.Int64Type:
			res[k] = v.(types.Int64).ValueInt64()
		case types.BoolType:
			res[k] = v.(types.Bool).ValueBool()
		default:
			//handle objects
			ret, err := objectToMap(v.(types.Object))
			if err != nil {
				return nil, err
			}
			res[k] = ret
		}
	}

	return res, nil
}

func mapToObject(m map[string]any) (*types.Object, error) {
	tMap := map[string]attr.Type{}
	vMap := map[string]attr.Value{}

	for k, v := range m {
		switch reflect.TypeOf(v).Kind() {
		case reflect.String:
			tMap[k] = types.StringType
			vMap[k] = types.StringValue(v.(string))
		case reflect.Int64:
			tMap[k] = types.Int64Type
			vMap[k] = types.Int64Value(v.(int64))
		case reflect.Int:
			tMap[k] = types.Int64Type
			vMap[k] = types.Int64Value(int64(v.(int)))
		case reflect.Bool:
			tMap[k] = types.BoolType
			vMap[k] = types.BoolValue(v.(bool))
		case reflect.Map:
			nested, err := mapToObject(m[k].(map[string]any))
			if err != nil {
				return nil, err
			}
			tMap[k] = types.ObjectType{
				AttrTypes: nested.AttributeTypes(context.Background()),
			}
			vMap[k] = *nested
		default:
			panic(fmt.Sprintf("unhandled type conversion %v", reflect.TypeOf(v).Kind()))
		}
	}

	obj, diag := types.ObjectValue(tMap, vMap)
	if diag.HasError() {
		return nil, errors.New("error converting proto struct to types.Object")
	}

	return &obj, nil
}

func ProtoStructToObject(s *structpb.Struct) (*types.Object, error) {
	return mapToObject(s.AsMap())
}

func ProtoAnyToObject(a *anypb.Any) (*types.Object, error) {
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

	return mapToObject(val)
}

//TODO - ProtoConcreteToObject
