package util

import (
	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/stretchr/testify/assert"
	"gitlab.authwise.io/authwise/api-client-go/authwise/types/core/v1alpha1"
	"testing"
)

func TestJsonToMap(t *testing.T) {
	type s struct {
		arrange func() jsontypes.Normalized
		assert  func(got map[string]any, err error)
	}

	cases := map[string]s{
		"all_types": {
			arrange: func() jsontypes.Normalized {
				jsonString := `{
					"attr1": "test",
					"attr2": 20,
					"attr3": {
						"nested": "nested_val"
					},
					"attr4": false,
					"attr5": [
						"test",
						"test2"
					],
					"attr6": [
						20,
						40
					]
				}`
				return jsontypes.NewNormalizedValue(jsonString)
			},
			assert: func(got map[string]any, err error) {
				expected := map[string]any{
					"attr1": "test",
					"attr2": float64(20),
					"attr3": map[string]any{
						"nested": "nested_val",
					},
					"attr4": false,
					"attr5": []any{
						"test",
						"test2",
					},
					"attr6": []any{
						float64(20),
						float64(40),
					},
				}
				assert.Nil(t, err)
				assert.Equal(t, expected, got)
			},
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			j := v.arrange()

			got, err := jsonToMap(j)

			v.assert(got, err)

		})
	}
}

//func TestObjectToMap(t *testing.T) {
//
//	type s struct {
//		arrange func() types.Object
//		assert  func(got map[string]any, err error)
//	}
//
//	cases := map[string]s{
//		"all_types": {
//			arrange: func() types.Object {
//				nestedTypes := map[string]attr.Type{
//					"nested": types.StringType,
//				}
//				nested := map[string]attr.Value{
//					"nested": types.StringValue("nested_val"),
//				}
//				elemTypes := map[string]attr.Type{
//					"attr1": types.StringType,
//					"attr2": types.Int64Type,
//					"attr3": types.ObjectType{
//						AttrTypes: nestedTypes,
//					},
//					"attr4": types.BoolType,
//					"attr5": types.ListType{
//						ElemType: types.StringType,
//					},
//					"attr6": types.ListType{
//						ElemType: types.Int64Type,
//					},
//				}
//				stringListValues := []attr.Value{
//					types.StringValue("test"),
//					types.StringValue("test2"),
//				}
//				intListValues := []attr.Value{
//					types.Int64Value(20),
//					types.Int64Value(40),
//				}
//				elems := map[string]attr.Value{
//					"attr1": types.StringValue("test"),
//					"attr2": types.Int64Value(20),
//					"attr3": types.ObjectValueMust(nestedTypes, nested),
//					"attr4": types.BoolValue(false),
//					"attr5": types.ListValueMust(types.StringType, stringListValues),
//					"attr6": types.ListValueMust(types.Int64Type, intListValues),
//				}
//
//				return types.ObjectValueMust(elemTypes, elems)
//			},
//			assert: func(got map[string]any, err error) {
//				expected := map[string]any{
//					"attr1": "test",
//					"attr2": int64(20),
//					"attr3": map[string]any{
//						"nested": "nested_val",
//					},
//					"attr4": false,
//					"attr5": []string{
//						"test",
//						"test2",
//					},
//					"attr6": []int{
//						20,
//						40,
//					},
//				}
//				assert.Nil(t, err)
//				assert.Equal(t, expected, got)
//			},
//		},
//	}
//
//	for k, v := range cases {
//		t.Run(k, func(t *testing.T) {
//			obj := v.arrange()
//
//			got, err := objectToMap(obj)
//
//			v.assert(got, err)
//
//		})
//	}
//}

func TestJsonToConcrete(t *testing.T) {

	type s[T any] struct {
		arrange func() jsontypes.Normalized
		assert  func(got *T, err error)
	}

	cases := map[string]s[v1alpha1.TenantUrlConfig]{
		"concrete": {
			arrange: func() jsontypes.Normalized {

				jsonString := `{ "cookie_domain": "test" }`

				return jsontypes.NewNormalizedValue(jsonString)
			},
			assert: func(got *v1alpha1.TenantUrlConfig, err error) {

				expected := &v1alpha1.TenantUrlConfig{
					CookieDomain: "test",
				}
				assert.Nil(t, err)
				assert.Equal(t, expected.CookieDomain, got.CookieDomain)
			},
		},
		"empty": {
			arrange: func() jsontypes.Normalized {

				jsonString := ``

				return jsontypes.NewNormalizedValue(jsonString)
			},
			assert: func(got *v1alpha1.TenantUrlConfig, err error) {

				expected := &v1alpha1.TenantUrlConfig{}
				assert.Nil(t, err)
				assert.Equal(t, expected.CookieDomain, got.CookieDomain)
			},
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			j := v.arrange()

			got, err := JsonToProtoConcrete[v1alpha1.TenantUrlConfig](j)

			v.assert(got, err)
		})
	}
}

//func TestMapToObject(t *testing.T) {
//
//	type s struct {
//		arrange func() map[string]any
//		assert  func(got types.Object, err error)
//	}
//
//	cases := map[string]s{
//		"all_data_types": {
//			arrange: func() map[string]any {
//
//				return map[string]any{
//					"attr1": "test",
//					"attr2": 20,
//					"attr3": false,
//					"attr4": map[string]any{
//						"attr4_nested1": "nested1",
//						"attr4_nested2": false,
//					},
//					"attr5": []string{
//						"test1",
//						"test2",
//					},
//				}
//			},
//			assert: func(got types.Object, err error) {
//				nestedTypes := map[string]attr.Type{
//					"attr4_nested1": types.StringType,
//					"attr4_nested2": types.BoolType,
//				}
//				nested := map[string]attr.Value{
//					"attr4_nested1": types.StringValue("nested1"),
//					"attr4_nested2": types.BoolValue(false),
//				}
//
//				elemTypes := map[string]attr.Type{
//					"attr1": types.StringType,
//					"attr2": types.Int64Type,
//					"attr3": types.BoolType,
//					"attr4": types.ObjectType{
//						AttrTypes: nestedTypes,
//					},
//					"attr5": types.ListType{
//						ElemType: types.StringType,
//					},
//				}
//				stringListValues := []attr.Value{
//					types.StringValue("test1"),
//					types.StringValue("test2"),
//				}
//				elems := map[string]attr.Value{
//					"attr1": types.StringValue("test"),
//					"attr2": types.Int64Value(20),
//					"attr3": types.BoolValue(false),
//					"attr4": types.ObjectValueMust(nestedTypes, nested),
//					"attr5": types.ListValueMust(types.StringType, stringListValues),
//				}
//
//				expected := types.ObjectValueMust(elemTypes, elems)
//				assert.Nil(t, err)
//				assert.Equal(t, expected, got)
//			},
//		},
//	}
//
//	for k, v := range cases {
//		t.Run(k, func(t *testing.T) {
//			obj := v.arrange()
//
//			got, err := mapToObject(obj)
//
//			v.assert(*got, err)
//
//		})
//	}
//}
