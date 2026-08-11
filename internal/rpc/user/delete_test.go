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
//   - v1.0.0: 用户删除 RPC 状态流转与权限校验单元测试
// ----------------------------------------------------------------------------

package user

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/controller"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"
	"github.com/openimsdk/protocol/auth"
	pbauth "github.com/openimsdk/protocol/auth"
	pbuser "github.com/openimsdk/protocol/user"
	"github.com/openimsdk/tools/mcontext"
	"google.golang.org/grpc"
)

type fakeUserDatabase struct {
	controller.UserDatabase
	users map[string]*model.User
}

func (f *fakeUserDatabase) GetUserByID(ctx context.Context, userID string) (*model.User, error) {
	if u, ok := f.users[userID]; ok {
		return u, nil
	}
	return nil, servererrs.ErrUserIDNotFound
}

func (f *fakeUserDatabase) UpdateByMap(ctx context.Context, userID string, args map[string]any) error {
	u := f.users[userID]
	for k, v := range args {
		switch k {
		case "status":
			u.Status = v.(int32)
		case "nickname":
			u.Nickname = v.(string)
		case "face_url":
			u.FaceURL = v.(string)
		}
	}
	return nil
}

func (f *fakeUserDatabase) DeleteUser(ctx context.Context, userID string) error {
	delete(f.users, userID)
	return nil
}

type mockAuthClient struct {
	auth.AuthClient
	logoutCalls int32
}

func (m *mockAuthClient) ForceLogout(ctx context.Context, in *pbauth.ForceLogoutReq, opts ...grpc.CallOption) (*pbauth.ForceLogoutResp, error) {
	atomic.AddInt32(&m.logoutCalls, 1)
	return &pbauth.ForceLogoutResp{}, nil
}

func newTestUserServer() (*userServer, *fakeUserDatabase) {
	db := &fakeUserDatabase{users: map[string]*model.User{}}
	s := &userServer{
		config:     &Config{Share: func() config.Share { s := config.Share{}; s.IMAdminUser.UserIDs = []string{"admin1"}; return s }()},
		db:         db,
		authClient: &rpcli.AuthClient{AuthClient: &mockAuthClient{}},
	}
	return s, db
}

func adminCtx() context.Context {
	ctx := mcontext.SetOpUserID(context.Background(), "admin1")
	return authverify.WithIMAdminUserIDs(ctx, []string{"admin1"})
}

func TestCheckArchivedState(t *testing.T) {
	s, _ := newTestUserServer()
	cases := []struct {
		name   string
		status int32
		wantOK bool
	}{
		{"normal is not archived", model.UserStatusNormal, false},
		{"archived is ok", model.UserStatusArchived, true},
		{"deleted is not archived", model.UserStatusDeleted, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := &model.User{UserID: "u1", Status: tc.status}
			err := s.checkArchived(u, u.UserID)
			if (err == nil) != tc.wantOK {
				t.Fatalf("checkArchived(status=%d) err=%v, wantOK=%v", tc.status, err, tc.wantOK)
			}
		})
	}
}

func TestArchiveUserStateTransitions(t *testing.T) {
	s, db := newTestUserServer()
	ctx := adminCtx()
	base := &model.User{UserID: "u_arch", Nickname: "tester", Status: model.UserStatusNormal}
	db.users[base.UserID] = base

	if _, err := s.ArchiveUser(ctx, &pbuser.ArchiveUserReq{UserID: base.UserID, Reason: "compliance"}); err != nil {
		t.Fatalf("archive normal user failed: %v", err)
	}
	if base.Status != model.UserStatusArchived {
		t.Fatalf("expected status archived, got %d", base.Status)
	}

	// archiving twice must fail.
	if _, err := s.ArchiveUser(ctx, &pbuser.ArchiveUserReq{UserID: base.UserID}); err == nil {
		t.Fatal("expected re-archive to fail")
	}

	// unarchive restores the normal state.
	if _, err := s.UnarchiveUser(ctx, &pbuser.UnarchiveUserReq{UserID: base.UserID}); err != nil {
		t.Fatalf("unarchive failed: %v", err)
	}
	if base.Status != model.UserStatusNormal {
		t.Fatalf("expected status normal after unarchive, got %d", base.Status)
	}

	// unarchive a normal user must fail.
	if _, err := s.UnarchiveUser(ctx, &pbuser.UnarchiveUserReq{UserID: base.UserID}); err == nil {
		t.Fatal("expected unarchive of a normal user to fail")
	}

	// permanent-delete requires the archived state first.
	if _, err := s.PermanentDeleteUser(ctx, &pbuser.PermanentDeleteUserReq{UserID: base.UserID}); err == nil {
		t.Fatal("expected permanent delete of a normal user to fail")
	}
}

func TestDeleteRequiresAdmin(t *testing.T) {
	s, _ := newTestUserServer()
	// non-admin caller.
	ctx := mcontext.SetOpUserID(context.Background(), "normal_user")
	for name, call := range map[string]func(context.Context) error{
		"ArchiveUser": func(ctx context.Context) error {
			_, err := s.ArchiveUser(ctx, &pbuser.ArchiveUserReq{UserID: "x"})
			return err
		},
		"UnarchiveUser": func(ctx context.Context) error {
			_, err := s.UnarchiveUser(ctx, &pbuser.UnarchiveUserReq{UserID: "x"})
			return err
		},
		"PermanentDeleteUser": func(ctx context.Context) error {
			_, err := s.PermanentDeleteUser(ctx, &pbuser.PermanentDeleteUserReq{UserID: "x"})
			return err
		},
		"MarkedDeleteUser": func(ctx context.Context) error {
			_, err := s.MarkedDeleteUser(ctx, &pbuser.MarkedDeleteUserReq{UserID: "x"})
			return err
		},
		"GetArchivedUsers": func(ctx context.Context) error {
			_, err := s.GetArchivedUsers(ctx, &pbuser.GetArchivedUsersReq{})
			return err
		},
		"GetDeletedUsers": func(ctx context.Context) error {
			_, err := s.GetDeletedUsers(ctx, &pbuser.GetDeletedUsersReq{})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(ctx); err == nil {
				t.Fatal("expected permission error for non-admin caller")
			}
		})
	}
}
