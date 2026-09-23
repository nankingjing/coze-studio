/*
 * Copyright 2025 coze-dev Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package vikingdb

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/components/retriever"
	"github.com/stretchr/testify/require"
	"github.com/volcengine/volc-sdk-golang/service/vikingdb"

	"github.com/coze-dev/coze-studio/backend/infra/document/searchstore"
	"github.com/coze-dev/coze-studio/backend/pkg/lang/ptr"
)

// genFilterStore is the smallest store genFilter needs: a collection declaring one
// string field to use as the partition key.
func genFilterStore() *vkSearchStore {
	return &vkSearchStore{
		collection: &vikingdb.Collection{
			Fields: []vikingdb.Field{
				{FieldName: "tenant", FieldType: vikingdb.String},
			},
		},
	}
}

func genFilterPartitionOptions() *searchstore.RetrieverOptions {
	return &searchstore.RetrieverOptions{
		PartitionKey: ptr.Of("tenant"),
		Partitions:   []string{"acme"},
	}
}

// A DSL filter resolved from the request and the partition condition describe two
// different things - "status" selects the documents, "tenant" selects the shard - so
// both must end up in the filter. Returning only one of them silently queries the
// wrong set: with the polarity inverted, adding a partition dropped the caller's
// own filter entirely.
func TestGenFilterKeepsDSLFilterWhenPartitioning(t *testing.T) {
	ctx := context.Background()
	co := &retriever.Options{
		DSLInfo: (&searchstore.DSL{Op: searchstore.OpEq, Field: "status", Value: "active"}).DSL(),
	}

	filter, err := genFilterStore().genFilter(ctx, co, genFilterPartitionOptions())
	require.NoError(t, err)
	require.NotNil(t, filter)

	require.Equal(t, "and", filter["op"])
	conds, ok := filter["conds"].([]map[string]any)
	require.True(t, ok, "a combined filter must carry a list of nodes, got %T", filter["conds"])
	require.Len(t, conds, 2, "both the DSL filter and the partition condition must be present")

	var sawDSL, sawPartition bool
	for _, cond := range conds {
		switch cond["field"] {
		case "status":
			sawDSL = true
			require.Equal(t, "must", cond["op"])
			require.Equal(t, []any{"active"}, cond["conds"])
		case "tenant":
			sawPartition = true
			require.Equal(t, "must", cond["op"])
			require.Equal(t, []string{"acme"}, cond["conds"])
		}
	}
	require.True(t, sawDSL, "the caller's DSL filter was dropped when the partition condition was merged in")
	require.True(t, sawPartition, "the partition condition is missing from the combined filter")
}

// With no DSL filter there is nothing to merge, so the partition condition is the
// whole filter - and it must be the partition node, not an "and" wrapping a nil.
func TestGenFilterUsesPartitionNodeWhenNoDSLFilter(t *testing.T) {
	ctx := context.Background()

	filter, err := genFilterStore().genFilter(ctx, &retriever.Options{}, genFilterPartitionOptions())
	require.NoError(t, err)
	require.NotNil(t, filter)

	require.Equal(t, "must", filter["op"])
	require.Equal(t, "tenant", filter["field"])
	require.Equal(t, []string{"acme"}, filter["conds"])
}

// Without a partition key the DSL filter must pass through untouched.
func TestGenFilterWithoutPartitionKeyIsUnchanged(t *testing.T) {
	ctx := context.Background()
	co := &retriever.Options{
		DSLInfo: (&searchstore.DSL{Op: searchstore.OpEq, Field: "status", Value: "active"}).DSL(),
	}

	filter, err := genFilterStore().genFilter(ctx, co, &searchstore.RetrieverOptions{})
	require.NoError(t, err)
	require.NotNil(t, filter)
	require.Equal(t, "must", filter["op"])
	require.Equal(t, "status", filter["field"])
}
