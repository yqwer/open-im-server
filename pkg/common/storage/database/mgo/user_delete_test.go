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
//   - v1.0.0: 用户删除机制状态流转集成测试
// ----------------------------------------------------------------------------

package mgo

import (
	"context"
	"testing"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	sdkws "github.com/openimsdk/protocol/sdkws"
)

func mustNewUserMgo(t *testing.T) database.User {
	t.Helper()
	db := newTestDatabase(t)
	userDB, err := NewUserMongo(db)
	if err != nil {
		t.Fatalf("NewUserMongo failed: %v", err)
	}
	return userDB
}

// TestUserStatusLifecycle verifies the whole user account state machine used by
// the user deletion mechanism:
//
//	normal -> archived -> (normal | deleted) -> physically removed.
func TestUserStatusLifecycle(t *testing.T) {
	ctx := context.Background()
	userDB := mustNewUserMgo(t)
	const userID = "user_delete_lifecycle"

	if err := userDB.Create(ctx, []*model.User{{UserID: userID, Nickname: "tester"}}); err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	// 1. a fresh account is normal.
	total, users, err := userDB.PageByStatus(ctx, model.UserStatusNormal, &sdkws.RequestPagination{PageNumber: 1, ShowNumber: 20})
	if err != nil {
		t.Fatalf("PageByStatus(normal) failed: %v", err)
	}
	if total != 1 || users[0].UserID != userID {
		t.Fatalf("expected the created user as normal, got total=%d users=%v", total, users)
	}

	// 2. archive it.
	now := time.Now()
	if err := userDB.UpdateByMap(ctx, userID, map[string]any{
		"status":         model.UserStatusArchived,
		"delete_user_id": "admin1",
		"delete_reason":  "test archive",
		"delete_time":    now,
	}); err != nil {
		t.Fatalf("archive user failed: %v", err)
	}
	total, users, err = userDB.PageByStatus(ctx, model.UserStatusArchived, &sdkws.RequestPagination{PageNumber: 1, ShowNumber: 20})
	if err != nil {
		t.Fatalf("PageByStatus(archived) failed: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected 1 archived user, got %d", total)
	}
	if got := users[0]; got.Status != model.UserStatusArchived || got.DeleteReason != "test archive" || got.DeleteUserID != "admin1" {
		t.Fatalf("archived user fields mismatch: %+v", got)
	}

	// 3. unarchive back to normal, archive metadata must be cleared.
	if err := userDB.UpdateByMap(ctx, userID, map[string]any{
		"status":         model.UserStatusNormal,
		"delete_user_id": "",
		"delete_reason":  "",
		"delete_time":    time.Time{},
	}); err != nil {
		t.Fatalf("unarchive user failed: %v", err)
	}
	total, _, err = userDB.PageByStatus(ctx, model.UserStatusArchived, &sdkws.RequestPagination{PageNumber: 1, ShowNumber: 20})
	if err != nil {
		t.Fatalf("PageByStatus(archived) after unarchive failed: %v", err)
	}
	if total != 0 {
		t.Fatalf("expected 0 archived users after unarchive, got %d", total)
	}
	u, err := userDB.Find(ctx, []string{userID})
	if err != nil {
		t.Fatalf("Find user failed: %v", err)
	}
	if len(u) != 1 || !u[0].IsNormal() {
		t.Fatalf("expected the user back to normal, got %+v", u)
	}

	// 4. marked-delete: account is anonymized and locked, record still exists.
	if err := userDB.UpdateByMap(ctx, userID, map[string]any{
		"status":              model.UserStatusDeleted,
		"nickname":            model.AnonymizedNickname,
		"face_url":            "",
		"ex":                  "",
		"global_recv_msg_opt": 2,
		"delete_user_id":      "admin1",
		"delete_time":         time.Now(),
	}); err != nil {
		t.Fatalf("marked-delete user failed: %v", err)
	}
	total, users, err = userDB.PageByStatus(ctx, model.UserStatusDeleted, &sdkws.RequestPagination{PageNumber: 1, ShowNumber: 20})
	if err != nil {
		t.Fatalf("PageByStatus(deleted) failed: %v", err)
	}
	if total != 1 || users[0].Nickname != model.AnonymizedNickname {
		t.Fatalf("expected 1 anonymized deleted user, got total=%d users=%+v", total, users)
	}
	u, err = userDB.Find(ctx, []string{userID})
	if err != nil {
		t.Fatalf("Find deleted user failed: %v", err)
	}
	if len(u) != 1 || u[0].IsNormal() {
		t.Fatalf("deleted user must not be reported normal: %+v", u)
	}

	// 5. physical delete removes the record for good.
	if err := userDB.Delete(ctx, userID); err != nil {
		t.Fatalf("physical delete user failed: %v", err)
	}
	users, err = userDB.FindByStatus(ctx, model.UserStatusDeleted)
	if err != nil {
		t.Fatalf("FindByStatus(deleted) failed: %v", err)
	}
	if len(users) != 0 {
		t.Fatalf("expected no deleted users after physical delete, got %d", len(users))
	}
	u, err = userDB.Find(ctx, []string{userID})
	if err != nil {
		t.Fatalf("Find after delete failed: %v", err)
	}
	if len(u) != 0 {
		t.Fatalf("expected the user record gone, got %+v", u)
	}
}

// TestPageByStatusPagination verifies paginated listing of users by status.
func TestPageByStatusPagination(t *testing.T) {
	ctx := context.Background()
	userDB := mustNewUserMgo(t)
	users := make([]*model.User, 0, 5)
	for i := 0; i < 5; i++ {
		users = append(users, &model.User{UserID: "paged_" + string(rune('a'+i)), Status: model.UserStatusArchived})
	}
	if err := userDB.Create(ctx, users); err != nil {
		t.Fatalf("create users failed: %v", err)
	}
	total, page1, err := userDB.PageByStatus(ctx, model.UserStatusArchived, &sdkws.RequestPagination{PageNumber: 1, ShowNumber: 2})
	if err != nil {
		t.Fatalf("PageByStatus failed: %v", err)
	}
	if total != 5 || len(page1) != 2 {
		t.Fatalf("expected total=5 page1=2, got total=%d page1=%d", total, len(page1))
	}
}
