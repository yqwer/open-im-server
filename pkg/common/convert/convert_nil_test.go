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
// 作者:      yqwer / Composer (Cursor Agent)
// 最后修订人: Composer (Cursor Agent)
// 变更说明:
//   - v1.0.0: 归档/删除用户被 GetDesignateUsers 过滤后，好友/申请/黑名单转换函数必须 nil 安全
// ----------------------------------------------------------------------------

package convert

import (
	"context"
	"testing"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/protocol/sdkws"
)

// getUsersWithMissing returns a fake user map provider that only contains
// "normal1"/"normal2", simulating GetDesignateUsers filtering out
// archived/marked-deleted accounts.
func getUsersWithMissing(ctx context.Context, userIDs []string) (map[string]*sdkws.UserInfo, error) {
	m := map[string]*sdkws.UserInfo{
		"normal1": {UserID: "normal1", Nickname: "normal-one", FaceURL: "http://n1"},
		"normal2": {UserID: "normal2", Nickname: "normal-two", FaceURL: "http://n2"},
	}
	return m, nil
}

func TestFriendsDB2PbNilSafeWhenFriendArchived(t *testing.T) {
	friends := []*model.Friend{
		{OwnerUserID: "me", FriendUserID: "normal1", CreateTime: time.Now(), IsPinned: true},
		// archived friend is filtered out of the user map -> must not panic.
		{OwnerUserID: "me", FriendUserID: "archived1", CreateTime: time.Now()},
	}
	res, err := FriendsDB2Pb(context.Background(), friends, getUsersWithMissing)
	if err != nil {
		t.Fatalf("FriendsDB2Pb failed: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2 friend records, got %d", len(res))
	}
	for _, f := range res {
		if f.FriendUser == nil {
			t.Fatalf("FriendUser must not be nil")
		}
	}
	if res[0].FriendUser.UserID != "normal1" || res[0].FriendUser.Nickname != "normal-one" {
		t.Fatalf("normal friend fields mismatch: %+v", res[0].FriendUser)
	}
	// Archived friend keeps only the id, nickname/face are empty.
	if res[1].FriendUser.UserID != "archived1" || res[1].FriendUser.Nickname != "" {
		t.Fatalf("archived friend should keep id only, got %+v", res[1].FriendUser)
	}
}

func TestFriendRequestDB2PbNilSafeWhenUserArchived(t *testing.T) {
	reqs := []*model.FriendRequest{
		{FromUserID: "normal1", ToUserID: "archived1", HandleResult: 1, CreateTime: time.Now()},
	}
	res, err := FriendRequestDB2Pb(context.Background(), reqs, getUsersWithMissing)
	if err != nil {
		t.Fatalf("FriendRequestDB2Pb failed: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 request, got %d", len(res))
	}
	if res[0].FromNickname != "normal-one" {
		t.Fatalf("from nickname mismatch: %+v", res[0])
	}
	if res[0].ToNickname != "" {
		t.Fatalf("archived to-user nickname should be empty, got %q", res[0].ToNickname)
	}
}

func TestBlackDB2PbNilSafeWhenUserArchived(t *testing.T) {
	blacks := []*model.Black{
		{OwnerUserID: "me", BlockUserID: "archived1", CreateTime: time.Now()},
	}
	res, err := BlackDB2Pb(context.Background(), blacks, getUsersWithMissing)
	if err != nil {
		t.Fatalf("BlackDB2Pb failed: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 black record, got %d", len(res))
	}
	if res[0].BlackUserInfo == nil {
		t.Fatalf("BlackUserInfo must not be nil")
	}
	if res[0].BlackUserInfo.UserID != "archived1" || res[0].BlackUserInfo.Nickname != "" {
		t.Fatalf("archived black user should keep id only, got %+v", res[0].BlackUserInfo)
	}
}
