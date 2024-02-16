// Copyright 2022 Liam White
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package test

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSchema(t *testing.T) {
	s := GenSchemaTest(context.Background())

	t.Run("Field Annotations", func(*testing.T) {
		require.True(t, s.Attributes["required"].IsRequired())
		require.True(t, s.Attributes["str"].IsOptional())
	})

	t.Run("Field injection", func(*testing.T) {
		require.True(t, s.Attributes["inject_computed"].IsComputed())
		require.True(t, s.Attributes["inject_required"].IsRequired())
		require.True(t, s.Attributes["inject_optional"].IsOptional())
	})

	t.Run("Primitive types", func(*testing.T) {
		require.IsType(t, schema.BoolAttribute{}, s.Attributes["bool"])
		require.IsType(t, schema.Float64Attribute{}, s.Attributes["double"])
		require.IsType(t, schema.Float64Attribute{}, s.Attributes["float"])
		require.IsType(t, schema.Int64Attribute{}, s.Attributes["int_32"])
		require.IsType(t, schema.Int64Attribute{}, s.Attributes["int_64"])
		require.IsType(t, schema.StringAttribute{}, s.Attributes["str"])
		require.IsType(t, schema.StringAttribute{}, s.Attributes["bytes"])
	})

	t.Run("List with primitive type", func(*testing.T) {
		require.IsType(t, schema.ListAttribute{}, s.Attributes["string_list"])
		require.Equal(t, types.StringType, s.Attributes["string_list"].(schema.ListAttribute).ElementType)
	})

	t.Run("Map with primitive type", func(*testing.T) {
		require.IsType(t, schema.MapAttribute{}, s.Attributes["map"])
		require.Equal(t, types.StringType, s.Attributes["map"].(schema.MapAttribute).ElementType)
	})

	/*
		t.Run("Nested message", func(*testing.T) {
			// Single
			require.Equal(t, types.StringType, s.Attributes["nested"].Attributes.GetAttributes()["str"].GetType())

			// List
			require.Nil(t, schema.Attributes["nested_list"].Type)
			require.Equal(t, types.StringType, s.Attributes["nested_list"].Attributes.GetAttributes()["str"].GetType())

			// Map
			require.Nil(t, schema.Attributes["nested_map"].Type)
			require.Equal(t, types.StringType, s.Attributes["nested_map"].Attributes.GetAttributes()["str"].GetType())
		})

		t.Run("Enum", func(*testing.T) {
			require.Equal(t, types.Int64Type, schema.Attributes["mode"].Type)
		})

		t.Run("OneOfs", func(*testing.T) {
			require.Nil(t, schema.Attributes["branch1"].Type)
			require.Equal(t, types.StringType, schema.Attributes["branch1"].Attributes.GetAttributes()["str"].GetType())
			require.Nil(t, schema.Attributes["branch2"].Type)
			require.Equal(t, types.Int64Type, schema.Attributes["branch2"].Attributes.GetAttributes()["int32"].GetType())
			require.Nil(t, schema.Attributes["branch3"].Attributes)
			require.Equal(t, types.StringType, schema.Attributes["branch3"].Type)
		})

	*/
}

func TestSchemaMultipleFiles(t *testing.T) {
	s := GenSchemaTest2(context.Background())
	require.IsType(t, schema.StringAttribute{}, s.Attributes["str"])
}
