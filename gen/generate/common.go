package generate

import "google.golang.org/protobuf/reflect/protoreflect"

var typeTypeMap = map[protoreflect.Kind]string{
	protoreflect.StringKind: "StringType",
	protoreflect.BytesKind:  "StringType",
	protoreflect.Int32Kind:  "Int64Type",
	protoreflect.Int64Kind:  "Int64Type",
	protoreflect.EnumKind:   "Int64Type",
	protoreflect.FloatKind:  "Float64Type",
	protoreflect.DoubleKind: "Float64Type",
	protoreflect.BoolKind:   "BoolType",
}

var typeMap = map[protoreflect.Kind]string{
	protoreflect.StringKind: "String",
	protoreflect.BytesKind:  "String",
	protoreflect.Int32Kind:  "Int64",
	protoreflect.Int64Kind:  "Int64",
	protoreflect.EnumKind:   "Int64",
	protoreflect.FloatKind:  "Float64",
	protoreflect.DoubleKind: "Float64",
	protoreflect.BoolKind:   "Bool",
}
