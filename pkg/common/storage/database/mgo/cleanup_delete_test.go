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
//   - v1.0.0: 关系/会话批量清理集成测试（好友、黑名单、申请、会话）
// ----------------------------------------------------------------------------

package mgo

import (
	"context"
	"testing"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

type collHelper struct {
	coll *mongo.Collection
}

func (h *collHelper) count(ctx context.Context, filter bson.M) int64 {
	n, err := h.coll.CountDocuments(ctx, filter)
	if err != nil {
		panic(err)
	}
	return n
}

// TestCleanRelationsBatch verifies the batch relation cleanup used by the user
// deletion mechanism:
//   - friends owned by the user are removed;
//   - reversal friends are removed via per-owner deletes (simulated here);
//   - friend requests where the user is either side are removed;
//   - blacks owned by the user and blacks pointing at the user are removed.
func TestCleanRelationsBatch(t *testing.T) {
	ctx := context.Background()
	db := newTestDatabase(t)
	const victim = "user_delete_victim"
	const peer = "user_delete_peer"

	friendDB, err := NewFriendMongo(db)
	if err != nil {
		t.Fatalf("NewFriendMongo failed: %v", err)
	}
	if err := friendDB.Create(ctx, []*model.Friend{{OwnerUserID: victim, FriendUserID: peer}}); err != nil {
		t.Fatalf("create friend failed: %v", err)
	}
	if err := friendDB.Create(ctx, []*model.Friend{{OwnerUserID: peer, FriendUserID: victim}}); err != nil {
		t.Fatalf("create reversal friend failed: %v", err)
	}
	// unrelated friend must survive.
	if err := friendDB.Create(ctx, []*model.Friend{{OwnerUserID: peer, FriendUserID: "someone_else"}}); err != nil {
		t.Fatalf("create unrelated friend failed: %v", err)
	}

	friendRequestDB, err := NewFriendRequestMongo(db)
	if err != nil {
		t.Fatalf("NewFriendRequestMongo failed: %v", err)
	}
	if err := friendRequestDB.Create(ctx, []*model.FriendRequest{{FromUserID: victim, ToUserID: peer}}); err != nil {
		t.Fatalf("create friend request failed: %v", err)
	}
	if err := friendRequestDB.Create(ctx, []*model.FriendRequest{{FromUserID: peer, ToUserID: victim}}); err != nil {
		t.Fatalf("create reversal friend request failed: %v", err)
	}

	blackDB, err := NewBlackMongo(db)
	if err != nil {
		t.Fatalf("NewBlackMongo failed: %v", err)
	}
	if err := blackDB.Create(ctx, []*model.Black{{OwnerUserID: victim, BlockUserID: peer}}); err != nil {
		t.Fatalf("create black failed: %v", err)
	}
	if err := blackDB.Create(ctx, []*model.Black{{OwnerUserID: peer, BlockUserID: victim}}); err != nil {
		t.Fatalf("create reversal black failed: %v", err)
	}

	// simulate the orchestration in CleanUserAllRelations.
	if err := friendDB.DeleteOwnerFriendAll(ctx, victim); err != nil {
		t.Fatalf("DeleteOwnerFriendAll failed: %v", err)
	}
	if err := friendDB.Delete(ctx, peer, []string{victim}); err != nil {
		t.Fatalf("reversal friend delete failed: %v", err)
	}
	if err := friendRequestDB.DeleteAllByUser(ctx, victim); err != nil {
		t.Fatalf("DeleteAllByUser(friend request) failed: %v", err)
	}
	if err := blackDB.DeleteOwnerBlackAll(ctx, victim); err != nil {
		t.Fatalf("DeleteOwnerBlackAll failed: %v", err)
	}
	if err := blackDB.DeleteBlackAllByBlockUserID(ctx, victim); err != nil {
		t.Fatalf("DeleteBlackAllByBlockUserID failed: %v", err)
	}

	fc := collHelper{db.Collection(database.FriendName)}
	if n := fc.count(ctx, bson.M{"owner_user_id": victim}); n != 0 {
		t.Fatalf("expected no friends owned by victim, got %d", n)
	}
	if n := fc.count(ctx, bson.M{"owner_user_id": peer, "friend_user_id": victim}); n != 0 {
		t.Fatalf("expected reversal friend removed, got %d", n)
	}
	if n := fc.count(ctx, bson.M{"owner_user_id": peer, "friend_user_id": "someone_else"}); n != 1 {
		t.Fatalf("expected unrelated friend survives, got %d", n)
	}

	frc := collHelper{db.Collection(database.FriendRequestName)}
	if n := frc.count(ctx, bson.M{}); n != 0 {
		t.Fatalf("expected all friend requests of victim removed, got %d", n)
	}

	bc := collHelper{db.Collection(database.BlackName)}
	if n := bc.count(ctx, bson.M{}); n != 0 {
		t.Fatalf("expected all blacks related to victim removed, got %d", n)
	}
}

// TestCleanGroupRequestsAndConversations verifies group request cleanup and the
// deletion of all conversations owned by the user.
func TestCleanGroupRequestsAndConversations(t *testing.T) {
	ctx := context.Background()
	db := newTestDatabase(t)
	const victim = "user_delete_group_victim"

	groupRequestDB, err := NewGroupRequestMgo(db)
	if err != nil {
		t.Fatalf("NewGroupRequestMgo failed: %v", err)
	}
	requests := []*model.GroupRequest{
		{UserID: victim, GroupID: "g1"},
		{GroupID: "g2", HandleUserID: victim},
		{GroupID: "g3", InviterUserID: victim},
		{UserID: "someone_else", GroupID: "g4"},
	}
	if err := groupRequestDB.Create(ctx, requests); err != nil {
		t.Fatalf("create group requests failed: %v", err)
	}
	if err := groupRequestDB.DeleteAllByUser(ctx, victim); err != nil {
		t.Fatalf("DeleteAllByUser(group request) failed: %v", err)
	}
	grc := collHelper{db.Collection(database.GroupRequestName)}
	if n := grc.count(ctx, bson.M{}); n != 1 {
		t.Fatalf("expected only the unrelated request survives, got %d", n)
	}

	conversationDB, err := NewConversationMongo(db)
	if err != nil {
		t.Fatalf("NewConversationMongo failed: %v", err)
	}
	convs := []*model.Conversation{
		{OwnerUserID: victim, ConversationID: "si_a_b"},
		{OwnerUserID: victim, ConversationID: "g_grp"},
		{OwnerUserID: "someone_else", ConversationID: "si_x_y"},
	}
	if err := conversationDB.Create(ctx, convs); err != nil {
		t.Fatalf("create conversations failed: %v", err)
	}
	if err := conversationDB.DeleteOwnerUserAllConversations(ctx, victim); err != nil {
		t.Fatalf("DeleteOwnerUserAllConversations failed: %v", err)
	}
	cc := collHelper{db.Collection(database.ConversationName)}
	if n := cc.count(ctx, bson.M{"owner_user_id": victim}); n != 0 {
		t.Fatalf("expected no conversations owned by victim, got %d", n)
	}
	if n := cc.count(ctx, bson.M{"owner_user_id": "someone_else"}); n != 1 {
		t.Fatalf("expected unrelated conversation survives, got %d", n)
	}
}
