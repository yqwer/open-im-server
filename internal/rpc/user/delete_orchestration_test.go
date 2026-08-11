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
//   - v1.0.0: 删除流程跨服务编排调用链与执行顺序单元测试
// ----------------------------------------------------------------------------

package user

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"
	pbconversation "github.com/openimsdk/protocol/conversation"
	pbgroup "github.com/openimsdk/protocol/group"
	pbmsg "github.com/openimsdk/protocol/msg"
	pbpush "github.com/openimsdk/protocol/push"
	pbrelation "github.com/openimsdk/protocol/relation"
	pbuser "github.com/openimsdk/protocol/user"
	"github.com/openimsdk/tools/mcontext"
	"google.golang.org/grpc"
)

// callRecorder records the order in which mocked clients are invoked.
type callRecorder struct {
	mu   sync.Mutex
	call []string
}

func (r *callRecorder) record(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.call = append(r.call, name)
}

func (r *callRecorder) order() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.call))
	copy(out, r.call)
	return out
}

func (r *callRecorder) indexOf(name string) int {
	for i, n := range r.order() {
		if n == name {
			return i
		}
	}
	return -1
}

type mockRelationClient struct {
	pbrelation.FriendClient
	called  bool
	records *callRecorder
}

func (m *mockRelationClient) CleanUserAllRelations(ctx context.Context, in *pbrelation.CleanUserAllRelationsReq, opts ...grpc.CallOption) (*pbrelation.CleanUserAllRelationsResp, error) {
	m.called = true
	m.records.record("relation")
	return &pbrelation.CleanUserAllRelationsResp{}, nil
}

type mockGroupClient struct {
	pbgroup.GroupClient
	called  bool
	records *callRecorder
}

func (m *mockGroupClient) QuitUserAllGroups(ctx context.Context, in *pbgroup.QuitUserAllGroupsReq, opts ...grpc.CallOption) (*pbgroup.QuitUserAllGroupsResp, error) {
	m.called = true
	m.records.record("group")
	return &pbgroup.QuitUserAllGroupsResp{}, nil
}

type mockConversationClient struct {
	pbconversation.ConversationClient
	called  bool
	records *callRecorder
}

func (m *mockConversationClient) DeleteUserAllConversations(ctx context.Context, in *pbconversation.DeleteUserAllConversationsReq, opts ...grpc.CallOption) (*pbconversation.DeleteUserAllConversationsResp, error) {
	m.called = true
	m.records.record("conversation")
	return &pbconversation.DeleteUserAllConversationsResp{}, nil
}

type mockMsgClient struct {
	pbmsg.MsgClient
	called  bool
	records *callRecorder
}

func (m *mockMsgClient) PhysicalDeleteUserAllMsg(ctx context.Context, in *pbmsg.PhysicalDeleteUserAllMsgReq, opts ...grpc.CallOption) (*pbmsg.PhysicalDeleteUserAllMsgResp, error) {
	m.called = true
	m.records.record("msg")
	return &pbmsg.PhysicalDeleteUserAllMsgResp{}, nil
}

type mockPushClient struct {
	pbpush.PushMsgServiceClient
	called  int
	records *callRecorder
}

func (m *mockPushClient) DelUserPushToken(ctx context.Context, in *pbpush.DelUserPushTokenReq, opts ...grpc.CallOption) (*pbpush.DelUserPushTokenResp, error) {
	m.called++
	m.records.record("push")
	return &pbpush.DelUserPushTokenResp{}, nil
}

type orchestrationServer struct {
	*userServer
	relation     *mockRelationClient
	group        *mockGroupClient
	conversation *mockConversationClient
	msg          *mockMsgClient
	push         *mockPushClient
	db           *fakeUserDatabase
	records      *callRecorder
}

func newOrchestrationServer(t *testing.T, status int32) *orchestrationServer {
	t.Helper()
	records := &callRecorder{}
	relation := &mockRelationClient{records: records}
	group := &mockGroupClient{records: records}
	conversation := &mockConversationClient{records: records}
	msg := &mockMsgClient{records: records}
	push := &mockPushClient{records: records}
	db := &fakeUserDatabase{users: map[string]*model.User{
		"u_delete": {UserID: "u_delete", Nickname: "tester", Status: status},
	}}
	s := &orchestrationServer{
		userServer: &userServer{
			config:             &Config{Share: func() config.Share { s := config.Share{}; s.IMAdminUser.UserIDs = []string{"admin1"}; return s }()},
			db:                 db,
			relationClient:     &rpcli.RelationClient{FriendClient: relation},
			groupClient:        &rpcli.GroupClient{GroupClient: group},
			conversationClient: &rpcli.ConversationClient{ConversationClient: conversation},
			msgClient:          &rpcli.MsgClient{MsgClient: msg},
			pushClient:         &rpcli.PushMsgServiceClient{PushMsgServiceClient: push},
		},
		relation:     relation,
		group:        group,
		conversation: conversation,
		msg:          msg,
		push:         push,
		db:           db,
		records:      records,
	}
	return s
}

// TestPermanentDeleteUserOrchestration verifies that the permanent delete flow
// calls every cleanup step and that message cleanup happens BEFORE conversations
// are deleted (PhysicalDeleteUserAllMsg resolves the user's conversation list).
func TestPermanentDeleteUserOrchestration(t *testing.T) {
	s := newOrchestrationServer(t, model.UserStatusArchived)
	ctx := adminCtx()

	if _, err := s.PermanentDeleteUser(ctx, &pbuser.PermanentDeleteUserReq{UserID: "u_delete"}); err != nil {
		t.Fatalf("PermanentDeleteUser failed: %v", err)
	}
	if !s.relation.called {
		t.Error("expected CleanUserAllRelations to be called")
	}
	if !s.group.called {
		t.Error("expected QuitUserAllGroups to be called")
	}
	if !s.conversation.called {
		t.Error("expected DeleteUserAllConversations to be called")
	}
	if !s.msg.called {
		t.Error("expected PhysicalDeleteUserAllMsg to be called")
	}
	if s.push.called == 0 {
		t.Error("expected DelUserPushToken to be called")
	}
	if _, ok := s.db.users["u_delete"]; ok {
		t.Error("expected the user record to be physically removed")
	}

	order := s.records.order()
	msgIdx, convIdx := s.records.indexOf("msg"), s.records.indexOf("conversation")
	if msgIdx < 0 || convIdx < 0 {
		t.Fatalf("expected both msg and conversation cleanup, got order %v", order)
	}
	if msgIdx > convIdx {
		t.Fatalf("expected message cleanup before conversation deletion, got order %v", order)
	}
}

func TestMarkedDeleteUserOrchestration(t *testing.T) {
	s := newOrchestrationServer(t, model.UserStatusArchived)
	ctx := adminCtx()

	if _, err := s.MarkedDeleteUser(ctx, &pbuser.MarkedDeleteUserReq{UserID: "u_delete"}); err != nil {
		t.Fatalf("MarkedDeleteUser failed: %v", err)
	}
	if !s.relation.called {
		t.Error("expected CleanUserAllRelations to be called")
	}
	if !s.group.called {
		t.Error("expected QuitUserAllGroups to be called")
	}
	if !s.conversation.called {
		t.Error("expected DeleteUserAllConversations to be called")
	}
	if s.msg.called {
		t.Error("expected PhysicalDeleteUserAllMsg NOT to be called for marked delete")
	}
	if s.push.called == 0 {
		t.Error("expected DelUserPushToken to be called")
	}
	// messages are kept, only the profile is anonymized and the state locked.
	u := s.db.users["u_delete"]
	if u == nil {
		t.Fatal("expected the user record to be kept")
	}
	if u.Status != model.UserStatusDeleted {
		t.Fatalf("expected status deleted, got %d", u.Status)
	}
	if !strings.Contains(u.Nickname, "注销") {
		t.Fatalf("expected anonymized nickname, got %q", u.Nickname)
	}
}

func TestPermanentDeleteRequiresArchived(t *testing.T) {
	s := newOrchestrationServer(t, model.UserStatusNormal)
	ctx := mcontext.SetOpUserID(context.Background(), "admin1")
	if _, err := s.PermanentDeleteUser(ctx, &pbuser.PermanentDeleteUserReq{UserID: "u_delete"}); err == nil {
		t.Fatal("expected permanent delete of a normal user to fail")
	}
	if s.relation.called {
		t.Error("expected no cleanup for a non-archived user")
	}
}
