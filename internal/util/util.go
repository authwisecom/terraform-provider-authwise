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

// TODO - add cases for each list type
func objectToMap(obj types.Object) (map[string]any, error) {
	ctx := context.Background()
	attrs := obj.Attributes()
	res := map[string]any{}

	for k, v := range attrs {
		switch v.Type(ctx) {
		case types.StringType:
			res[k] = v.(types.String).ValueString()
		case types.Int64Type:
			res[k] = v.(types.Int64).ValueInt64()
		case types.BoolType:
			res[k] = v.(types.Bool).ValueBool()
		case types.ListType{
			ElemType: types.StringType,
		}:
			elements := make([]string, 0, len(v.(types.List).Elements()))
			diag := v.(types.List).ElementsAs(ctx, &elements, false)
			if diag.HasError() {
				return res, errors.New("can not convert types.list(types.string) to []string")
			}
			res[k] = elements
		case types.ListType{
			ElemType: types.Int64Type,
		}:
			elements := make([]int, 0, len(v.(types.List).Elements()))
			diag := v.(types.List).ElementsAs(ctx, &elements, false)
			if diag.HasError() {
				return res, errors.New("can not convert types.list(types.int64) to []int64")
			}
			res[k] = elements
		default:
			fmt.Printf("test: %v", v.Type(ctx))
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

func sliceToList(a any) (*types.List, error) {

	t := reflect.TypeOf(a).Elem()
	l := reflect.ValueOf(a)
	ctx := context.Background()

	switch t.Kind() {
	case reflect.String:
		slice, diag := types.ListValueFrom(ctx, types.StringType, l.Interface().([]string))
		if diag.HasError() {
			return nil, errors.New("unable to convert slice to []types.string")
		}
		return &slice, nil
	case reflect.Int64, reflect.Int32:
		slice, diag := types.ListValueFrom(ctx, types.StringType, l.Interface().([]int))
		if diag.HasError() {
			return nil, errors.New("unable to convert slice to []types.int64")
		}
		return &slice, nil
	case reflect.Bool:
		slice, diag := types.ListValueFrom(ctx, types.StringType, l.Interface().([]bool))
		if diag.HasError() {
			return &slice, errors.New("unable to convert slice to []types.bool")
		}
		return &slice, nil
	default:
		return nil, errors.New("unexpected type during slice conversion")
	}
}

func mapToObject(m map[string]any) (*types.Object, error) {
	tMap := map[string]attr.Type{}
	vMap := map[string]attr.Value{}
	ctx := context.Background()

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
		case reflect.Slice:
			test, err := sliceToList(v)
			if err != nil {
				return nil, err
			}
			tMap[k] = test.Type(ctx)
			vMap[k], _ = test.ToListValue(ctx)
		case reflect.Map:
			nested, err := mapToObject(m[k].(map[string]any))
			if err != nil {
				return nil, err
			}
			tMap[k] = types.ObjectType{
				AttrTypes: nested.AttributeTypes(ctx),
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
