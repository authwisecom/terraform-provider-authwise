package util

import (
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestObjectToMap(t *testing.T) {

	type s struct {
		arrange func() types.Object
		assert  func(got map[string]any, err error)
	}

	cases := map[string]s{
		"test_all_types": {
			arrange: func() types.Object {
				nestedTypes := map[string]attr.Type{
					"nested": types.StringType,
				}
				nested := map[string]attr.Value{
					"nested": types.StringValue("nested_val"),
				}
				elemTypes := map[string]attr.Type{
					"attr1": types.StringType,
					"attr2": types.Int64Type,
					"attr3": types.ObjectType{
						AttrTypes: nestedTypes,
					},
					"attr4": types.BoolType,
				}
				elems := map[string]attr.Value{
					"attr1": types.StringValue("test"),
					"attr2": types.Int64Value(20),
					"attr3": types.ObjectValueMust(nestedTypes, nested),
					"attr4": types.BoolValue(false),
				}

				return types.ObjectValueMust(elemTypes, elems)
			},
			assert: func(got map[string]any, err error) {
				expected := map[string]any{
					"attr1": "test",
					"attr2": int64(20),
					"attr3": map[string]any{
						"nested": "nested_val",
					},
					"attr4": false,
				}
				assert.Nil(t, err)
				assert.Equal(t, expected, got)
			},
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			obj := v.arrange()

			got, err := objectToMap(obj)

			v.assert(got, err)

		})
	}
}
