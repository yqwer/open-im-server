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
//   - v1.0.0: 消息真物理删除与群聊发送者匿名化集成测试
// ----------------------------------------------------------------------------

package mgo

import (
	"context"
	"testing"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"go.mongodb.org/mongo-driver/bson"
)

func mustNewMsgMgo(t *testing.T) database.Msg {
	t.Helper()
	db := newTestDatabase(t)
	msgDB, err := NewMsgMongo(db)
	if err != nil {
		t.Fatalf("NewMsgMongo failed: %v", err)
	}
	return msgDB
}

func newMsgDoc(docID, sendID, nickname, faceURL string) *model.MsgDocModel {
	return &model.MsgDocModel{
		DocID: docID,
		Msg: []*model.MsgInfoModel{
			{
				Msg: &model.MsgDataModel{
					SendID:         sendID,
					SenderNickname: nickname,
					SenderFaceURL:  faceURL,
					ClientMsgID:    "cm_" + docID + "_" + sendID,
				},
			},
		},
	}
}

// TestMsgPhysicalDelete verifies that single-chat message docs are physically
// removed for the deleted user's conversations while group message docs survive.
func TestMsgPhysicalDelete(t *testing.T) {
	ctx := context.Background()
	msgDB := mustNewMsgMgo(t)
	const (
		singleConv = "si_user_delete_a_user_delete_b"
		groupConv  = "g_group_delete_test"
		deletedUID = "user_delete_a"
		otherUID   = "user_delete_c"
	)

	if err := msgDB.Create(ctx, newMsgDoc(singleConv+":0", deletedUID, "a", "http://a")); err != nil {
		t.Fatalf("create single chat doc failed: %v", err)
	}
	if err := msgDB.Create(ctx, newMsgDoc(groupConv+":0", deletedUID, "a", "http://a")); err != nil {
		t.Fatalf("create group doc (deleted sender) failed: %v", err)
	}
	if err := msgDB.Create(ctx, newMsgDoc(groupConv+":1", otherUID, "c", "http://c")); err != nil {
		t.Fatalf("create group doc (other sender) failed: %v", err)
	}

	if err := msgDB.DeleteDocsByConversationIDs(ctx, []string{singleConv}); err != nil {
		t.Fatalf("DeleteDocsByConversationIDs failed: %v", err)
	}

	// single-chat docs must be gone.
	if doc, err := msgDB.FindOneByDocID(ctx, singleConv+":0"); err == nil || doc != nil {
		t.Fatalf("expected single chat doc physically deleted, got doc=%+v err=%v", doc, err)
	}
	// group docs must remain.
	if doc, err := msgDB.FindOneByDocID(ctx, groupConv+":0"); err != nil || doc == nil {
		t.Fatalf("expected group doc to survive, got err=%v", err)
	}
	if doc, err := msgDB.FindOneByDocID(ctx, groupConv+":1"); err != nil || doc == nil {
		t.Fatalf("expected other group doc to survive, got err=%v", err)
	}
}

// TestAnonymizeConversationSender verifies that group message senders are
// anonymized: send_id is replaced, nickname becomes "已注销用户" and face url is
// cleared, while other senders are untouched.
func TestAnonymizeConversationSender(t *testing.T) {
	ctx := context.Background()
	msgDB := mustNewMsgMgo(t)
	const (
		groupConv  = "g_group_anon_test"
		deletedUID = "user_delete_anon"
		otherUID   = "user_delete_keep"
	)
	if err := msgDB.Create(ctx, newMsgDoc(groupConv+":0", deletedUID, "old-name", "http://old")); err != nil {
		t.Fatalf("create doc failed: %v", err)
	}
	if err := msgDB.Create(ctx, newMsgDoc(groupConv+":1", otherUID, "keeper", "http://keep")); err != nil {
		t.Fatalf("create doc failed: %v", err)
	}

	if err := msgDB.AnonymizeConversationSender(ctx, deletedUID, []string{groupConv}); err != nil {
		t.Fatalf("AnonymizeConversationSender failed: %v", err)
	}

	doc, err := msgDB.FindOneByDocID(ctx, groupConv+":0")
	if err != nil || doc == nil {
		t.Fatalf("expected the anon doc still exists, err=%v", err)
	}
	msg := doc.Msg[0].Msg
	if msg.SendID != model.AnonymizedSenderID {
		t.Fatalf("expected send_id=%q, got %q", model.AnonymizedSenderID, msg.SendID)
	}
	if msg.SenderNickname != model.AnonymizedNickname {
		t.Fatalf("expected nickname=%q, got %q", model.AnonymizedNickname, msg.SenderNickname)
	}
	if msg.SenderFaceURL != "" {
		t.Fatalf("expected face url cleared, got %q", msg.SenderFaceURL)
	}

	doc, err = msgDB.FindOneByDocID(ctx, groupConv+":1")
	if err != nil || doc == nil {
		t.Fatalf("expected the other doc exists, err=%v", err)
	}
	msg = doc.Msg[0].Msg
	if msg.SendID != otherUID {
		t.Fatalf("expected the other sender untouched, got %q", msg.SendID)
	}
	if msg.SenderNickname != "keeper" {
		t.Fatalf("expected the other nickname untouched, got %q", msg.SenderNickname)
	}
}

// TestDeleteDocsByConversationIDsNoMatch verifies that unrelated conversations
// are not touched when deleting message docs.
func TestDeleteDocsByConversationIDsNoMatch(t *testing.T) {
	ctx := context.Background()
	msgDB := mustNewMsgMgo(t)
	doc := newMsgDoc("si_keep_1:0", "u1", "n1", "")
	if err := msgDB.Create(ctx, doc); err != nil {
		t.Fatalf("create doc failed: %v", err)
	}
	if err := msgDB.DeleteDocsByConversationIDs(ctx, []string{"si_other_999"}); err != nil {
		t.Fatalf("DeleteDocsByConversationIDs failed: %v", err)
	}
	if doc, err := msgDB.FindOneByDocID(ctx, "si_keep_1:0"); err != nil || doc == nil {
		t.Fatalf("expected unrelated doc survives, err=%v", err)
	}
	// prefix collision guard: si_keep must not be deleted by si_keep_1
	if err := msgDB.DeleteDocsByConversationIDs(ctx, []string{"si_keep"}); err != nil {
		t.Fatalf("DeleteDocsByConversationIDs failed: %v", err)
	}
	if doc, err := msgDB.FindOneByDocID(ctx, "si_keep_1:0"); err != nil || doc == nil {
		t.Fatalf("expected prefix collision not to delete doc, err=%v", err)
	}
	_ = bson.M{} // keep import stable
}
