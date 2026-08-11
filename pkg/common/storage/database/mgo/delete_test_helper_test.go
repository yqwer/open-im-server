// Copyright © 2023 OpenIM. All rights reserved.
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
//
// ----------------------------------------------------------------------------
// 版本号:    v1.0.0
// 修订日期:  2026-07-31
// 作者:      yqwer / DeepSeek-V4
// 最后修订人: DeepSeek-V4
// 变更说明:
//   - v1.0.0: 用户删除机制集成测试公共辅助（独立测试库，用后即删）
// ----------------------------------------------------------------------------

package mgo

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// testMongoURI returns the mongo URI used by integration tests. It can be
// overridden through the TEST_MONGO_URI environment variable.
func testMongoURI() string {
	if uri := os.Getenv("TEST_MONGO_URI"); uri != "" {
		return uri
	}
	return "mongodb://root:openIM123@172.18.0.5:27017/?authSource=admin"
}

// newTestDatabase connects to mongo and returns a dedicated database that is
// dropped when the test finishes. Each call creates a unique database so tests
// never share data.
func newTestDatabase(t interface {
	Helper()
	Cleanup(func())
}) *mongo.Database {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cli, err := mongo.Connect(ctx, options.Client().ApplyURI(testMongoURI()))
	if err != nil {
		panic(fmt.Sprintf("connect test mongo failed: %v", err))
	}
	name := fmt.Sprintf("openim_v3_delete_test_%d", time.Now().UnixNano())
	db := cli.Database(name)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = db.Drop(ctx)
		_ = cli.Disconnect(ctx)
	})
	return db
}
