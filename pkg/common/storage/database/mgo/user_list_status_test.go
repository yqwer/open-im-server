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
// 版本号:    v1.1.0
// 修订日期:  2026-07-31
// 作者:      yqwer / Composer (Cursor Agent)
// 最后修订人: Composer (Cursor Agent)
// 变更说明:
//   - v1.0.0: 普通用户查询接口必须对 archived/deleted 用户不可见（status 过滤集成测试）
//   - v1.1.0: 补充缺 status 字段的存量用户可见性、Create 默认 status 写入测试
// ----------------------------------------------------------------------------

package mgo

import (
	"context"
	"testing"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	sdkws "github.com/openimsdk/protocol/sdkws"
	"go.mongodb.org/mongo-driver/bson"
)

const (
	testLevelOrdinary = 0
	testLevelApp      = 1
)

// seedUsersForStatusFiltering creates one normal, one archived and one
// marked-deleted user, returning their IDs.
func seedUsersForStatusFiltering(t *testing.T, userDB database.User) (normalID, archivedID, deletedID string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	normalID = "sf_normal"
	archivedID = "sf_archived"
	deletedID = "sf_deleted"
	users := []*model.User{
		{UserID: normalID, Nickname: "normal-user", CreateTime: now},
		{UserID: archivedID, Nickname: "archived-user", CreateTime: now},
		{UserID: deletedID, Nickname: model.AnonymizedNickname, CreateTime: now},
	}
	if err := userDB.Create(ctx, users); err != nil {
		t.Fatalf("create seed users failed: %v", err)
	}
	if err := userDB.UpdateByMap(ctx, archivedID, map[string]any{
		"status":        model.UserStatusArchived,
		"delete_time":   now,
		"delete_user_id": "admin1",
	}); err != nil {
		t.Fatalf("archive seed user failed: %v", err)
	}
	if err := userDB.UpdateByMap(ctx, deletedID, map[string]any{
		"status":        model.UserStatusDeleted,
		"delete_time":   now,
		"delete_user_id": "admin1",
	}); err != nil {
		t.Fatalf("mark-delete seed user failed: %v", err)
	}
	return normalID, archivedID, deletedID
}

func TestPageFindUserExcludesNonNormal(t *testing.T) {
	ctx := context.Background()
	userDB := mustNewUserMgo(t)
	normalID, archivedID, deletedID := seedUsersForStatusFiltering(t, userDB)
	_ = deletedID

	total, users, err := userDB.PageFindUser(ctx, testLevelOrdinary, testLevelApp, &sdkws.RequestPagination{PageNumber: 1, ShowNumber: 20})
	if err != nil {
		t.Fatalf("PageFindUser failed: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected only the normal user, got total=%d", total)
	}
	if users[0].UserID != normalID {
		t.Fatalf("expected %s in normal user list, got %v", normalID, users)
	}
	for _, u := range users {
		if u.UserID == archivedID {
			t.Fatalf("archived user %s must not appear in normal user list", archivedID)
		}
	}
}

func TestPageFindUserWithKeywordExcludesNonNormal(t *testing.T) {
	ctx := context.Background()
	userDB := mustNewUserMgo(t)
	normalID, archivedID, deletedID := seedUsersForStatusFiltering(t, userDB)

	// Keyword search by userID.
	total, users, err := userDB.PageFindUserWithKeyword(ctx, testLevelOrdinary, testLevelApp, "sf_", "", &sdkws.RequestPagination{PageNumber: 1, ShowNumber: 20})
	if err != nil {
		t.Fatalf("PageFindUserWithKeyword failed: %v", err)
	}
	if total != 1 || users[0].UserID != normalID {
		t.Fatalf("expected only %s, got total=%d users=%v", normalID, total, users)
	}

	// Keyword search by nickname.
	total, users, err = userDB.PageFindUserWithKeyword(ctx, testLevelOrdinary, testLevelApp, "", "archived", &sdkws.RequestPagination{PageNumber: 1, ShowNumber: 20})
	if err != nil {
		t.Fatalf("PageFindUserWithKeyword(nickname) failed: %v", err)
	}
	if total != 0 {
		t.Fatalf("archived user %s must not be searchable by nickname, got total=%d", archivedID, total)
	}
	_ = deletedID
}

func TestGetAllUserIDExcludesNonNormal(t *testing.T) {
	ctx := context.Background()
	userDB := mustNewUserMgo(t)
	normalID, archivedID, deletedID := seedUsersForStatusFiltering(t, userDB)

	total, userIDs, err := userDB.GetAllUserID(ctx, &sdkws.RequestPagination{PageNumber: 1, ShowNumber: 20})
	if err != nil {
		t.Fatalf("GetAllUserID failed: %v", err)
	}
	if total != 1 || userIDs[0] != normalID {
		t.Fatalf("expected only %s, got total=%d ids=%v", normalID, total, userIDs)
	}
	for _, id := range userIDs {
		if id == archivedID || id == deletedID {
			t.Fatalf("non-normal user %s must not appear in getAllUserID", id)
		}
	}
}

func TestCountTotalExcludesNonNormal(t *testing.T) {
	ctx := context.Background()
	userDB := mustNewUserMgo(t)
	_, _, _ = seedUsersForStatusFiltering(t, userDB)

	count, err := userDB.CountTotal(ctx, nil)
	if err != nil {
		t.Fatalf("CountTotal failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected total count 1 (normal only), got %d", count)
	}
}

func TestCountRangeEverydayTotalExcludesNonNormal(t *testing.T) {
	ctx := context.Background()
	userDB := mustNewUserMgo(t)
	_, _, _ = seedUsersForStatusFiltering(t, userDB)

	// All three seed users were created "now", within [now-1h, now+1h].
	start := time.Now().Add(-time.Hour)
	end := time.Now().Add(time.Hour)
	counts, err := userDB.CountRangeEverydayTotal(ctx, start, end)
	if err != nil {
		t.Fatalf("CountRangeEverydayTotal failed: %v", err)
	}
	total := int64(0)
	for _, c := range counts {
		total += c
	}
	if total != 1 {
		t.Fatalf("expected daily count 1 (normal only), got %v", counts)
	}
}

func TestSortQueryExcludesNonNormal(t *testing.T) {
	ctx := context.Background()
	userDB := mustNewUserMgo(t)
	normalID, archivedID, deletedID := seedUsersForStatusFiltering(t, userDB)

	users, err := userDB.SortQuery(ctx, map[string]string{
		normalID:   "",
		archivedID: "",
		deletedID:  "",
	}, true)
	if err != nil {
		t.Fatalf("SortQuery failed: %v", err)
	}
	if len(users) != 1 || users[0].UserID != normalID {
		t.Fatalf("expected only %s from SortQuery, got %v", normalID, users)
	}
}

// TestLegacyUsersWithoutStatusFieldVisible verifies that documents created
// before the deletion mechanism (no status field) are treated as normal and
// still appear in user list / getAllUserID queries.
func TestLegacyUsersWithoutStatusFieldVisible(t *testing.T) {
	ctx := context.Background()
	db := newTestDatabase(t)
	userDB, err := NewUserMongo(db)
	if err != nil {
		t.Fatalf("NewUserMongo failed: %v", err)
	}

	// Insert a legacy-shaped document that has no status field at all.
	_, err = db.Collection(database.UserName).InsertOne(ctx, bson.M{
		"user_id":          "legacy_no_status",
		"nickname":         "legacy-user",
		"face_url":         "",
		"ex":               "",
		"app_manger_level": 0,
		"create_time":      time.Now(),
	})
	if err != nil {
		t.Fatalf("insert legacy user failed: %v", err)
	}

	total, users, err := userDB.PageFindUser(ctx, testLevelOrdinary, testLevelApp, &sdkws.RequestPagination{PageNumber: 1, ShowNumber: 20})
	if err != nil {
		t.Fatalf("PageFindUser failed: %v", err)
	}
	if total != 1 || users[0].UserID != "legacy_no_status" {
		t.Fatalf("legacy user without status must be visible, got total=%d users=%v", total, users)
	}

	total, ids, err := userDB.GetAllUserID(ctx, &sdkws.RequestPagination{PageNumber: 1, ShowNumber: 20})
	if err != nil {
		t.Fatalf("GetAllUserID failed: %v", err)
	}
	if total != 1 || ids[0] != "legacy_no_status" {
		t.Fatalf("legacy user without status must appear in getAllUserID, got total=%d ids=%v", total, ids)
	}
}

// TestCreateWritesDefaultStatus verifies Create always persists status=0 for
// new normal users (so they never rely on "field absent").
func TestCreateWritesDefaultStatus(t *testing.T) {
	ctx := context.Background()
	userDB := mustNewUserMgo(t)
	if err := userDB.Create(ctx, []*model.User{{UserID: "new_default", Nickname: "n"}}); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	users, err := userDB.Find(ctx, []string{"new_default"})
	if err != nil || len(users) != 1 {
		t.Fatalf("Find failed: %v users=%v", err, users)
	}
	if users[0].Status != model.UserStatusNormal {
		t.Fatalf("expected status=0 after Create, got %d", users[0].Status)
	}
}
