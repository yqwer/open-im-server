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
//   - v1.0.0: 用户删除机制 RPC：归档/解除归档/物理删除/标记删除/分页查询
// ----------------------------------------------------------------------------

package user

import (
	"context"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/common/convert"
	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	pbauth "github.com/openimsdk/protocol/auth"
	pbconversation "github.com/openimsdk/protocol/conversation"
	pbgroup "github.com/openimsdk/protocol/group"
	pbmsg "github.com/openimsdk/protocol/msg"
	pbpush "github.com/openimsdk/protocol/push"
	pbrelation "github.com/openimsdk/protocol/relation"
	pbuser "github.com/openimsdk/protocol/user"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
)

// forceLogoutAll kicks a user offline from every supported platform.
func (s *userServer) forceLogoutAll(ctx context.Context, userID string) error {
	for platformID := int32(1); platformID <= 12; platformID++ {
		_, err := s.authClient.ForceLogout(ctx, &pbauth.ForceLogoutReq{UserID: userID, PlatformID: platformID})
		if err != nil {
			log.ZWarn(ctx, "forceLogoutAll platform failed", err, "userID", userID, "platformID", platformID)
		}
	}
	return nil
}

// clearPushToken removes the FCM push tokens of a user from every platform.
func (s *userServer) clearPushToken(ctx context.Context, userID string) error {
	for platformID := int32(1); platformID <= 12; platformID++ {
		_, err := s.pushClient.DelUserPushToken(ctx, &pbpush.DelUserPushTokenReq{UserID: userID, PlatformID: platformID})
		if err != nil {
			log.ZWarn(ctx, "clearPushToken platform failed", err, "userID", userID, "platformID", platformID)
		}
	}
	return nil
}

// getNormalUser returns the user record and requires it to exist.
func (s *userServer) getNormalUser(ctx context.Context, userID string) (*model.User, error) {
	user, err := s.db.GetUserByID(ctx, userID)
	if err != nil {
		return nil, errs.WrapMsg(err, "user not found", "userID", userID)
	}
	return user, nil
}

// checkArchived verifies that the user is currently in the archived state.
func (s *userServer) checkArchived(user *model.User, userID string) error {
	if user.Status != model.UserStatusArchived {
		return servererrs.ErrArgs.WrapMsg("user is not in archived state, current status", "userID", userID, "status", user.Status)
	}
	return nil
}

// ArchiveUser archives a normal user: mark the account archived and kick every device offline.
func (s *userServer) ArchiveUser(ctx context.Context, req *pbuser.ArchiveUserReq) (*pbuser.ArchiveUserResp, error) {
	if err := authverify.CheckAdmin(ctx); err != nil {
		return nil, err
	}
	operatorID := mcontext.GetOpUserID(ctx)
	user, err := s.getNormalUser(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	if user.Status != model.UserStatusNormal {
		return nil, servererrs.ErrArgs.WrapMsg("only a normal user can be archived", "userID", req.UserID, "status", user.Status)
	}
	now := time.Now()
	if err := s.db.UpdateByMap(ctx, req.UserID, map[string]any{
		"status":         model.UserStatusArchived,
		"delete_user_id": operatorID,
		"delete_reason":  req.Reason,
		"delete_time":    now,
	}); err != nil {
		log.ZError(ctx, "ArchiveUser UpdateByMap failed", err, "userID", req.UserID)
		return nil, err
	}
	if err := s.forceLogoutAll(ctx, req.UserID); err != nil {
		log.ZWarn(ctx, "ArchiveUser forceLogoutAll failed", err, "userID", req.UserID)
	}
	log.ZInfo(ctx, "audit user archived", "operator", operatorID, "userID", req.UserID, "reason", req.Reason, "time", now)
	return &pbuser.ArchiveUserResp{}, nil
}

// UnarchiveUser restores an archived user back to the normal state.
func (s *userServer) UnarchiveUser(ctx context.Context, req *pbuser.UnarchiveUserReq) (*pbuser.UnarchiveUserResp, error) {
	if err := authverify.CheckAdmin(ctx); err != nil {
		return nil, err
	}
	operatorID := mcontext.GetOpUserID(ctx)
	user, err := s.getNormalUser(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	if err := s.checkArchived(user, req.UserID); err != nil {
		return nil, err
	}
	if err := s.db.UpdateByMap(ctx, req.UserID, map[string]any{
		"status":         model.UserStatusNormal,
		"delete_user_id": "",
		"delete_reason":  "",
		"delete_time":    time.Time{},
	}); err != nil {
		log.ZError(ctx, "UnarchiveUser UpdateByMap failed", err, "userID", req.UserID)
		return nil, err
	}
	log.ZInfo(ctx, "audit user unarchived", "operator", operatorID, "userID", req.UserID, "time", time.Now())
	return &pbuser.UnarchiveUserResp{}, nil
}

// PermanentDeleteUser permanently deletes an archived user. All related data is
// cleaned up: relations, groups, conversations, push tokens and messages, and
// finally the user record itself is physically removed.
func (s *userServer) PermanentDeleteUser(ctx context.Context, req *pbuser.PermanentDeleteUserReq) (*pbuser.PermanentDeleteUserResp, error) {
	if err := authverify.CheckAdmin(ctx); err != nil {
		return nil, err
	}
	operatorID := mcontext.GetOpUserID(ctx)
	user, err := s.getNormalUser(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	if err := s.checkArchived(user, req.UserID); err != nil {
		return nil, err
	}

	if _, err := s.relationClient.CleanUserAllRelations(ctx, &pbrelation.CleanUserAllRelationsReq{UserID: req.UserID}); err != nil {
		log.ZError(ctx, "PermanentDeleteUser CleanUserAllRelations failed", err, "userID", req.UserID)
		return nil, err
	}
	if _, err := s.groupClient.QuitUserAllGroups(ctx, &pbgroup.QuitUserAllGroupsReq{UserID: req.UserID}); err != nil {
		log.ZError(ctx, "PermanentDeleteUser QuitUserAllGroups failed", err, "userID", req.UserID)
		return nil, err
	}
	// physical message cleanup must run before conversations are deleted, because
	// PhysicalDeleteUserAllMsg resolves the conversation list of the user.
	if _, err := s.msgClient.PhysicalDeleteUserAllMsg(ctx, &pbmsg.PhysicalDeleteUserAllMsgReq{UserID: req.UserID}); err != nil {
		log.ZError(ctx, "PermanentDeleteUser PhysicalDeleteUserAllMsg failed", err, "userID", req.UserID)
		return nil, err
	}
	if _, err := s.conversationClient.DeleteUserAllConversations(ctx, &pbconversation.DeleteUserAllConversationsReq{UserID: req.UserID}); err != nil {
		log.ZError(ctx, "PermanentDeleteUser DeleteUserAllConversations failed", err, "userID", req.UserID)
		return nil, err
	}
	if err := s.clearPushToken(ctx, req.UserID); err != nil {
		log.ZError(ctx, "PermanentDeleteUser clearPushToken failed", err, "userID", req.UserID)
		return nil, err
	}
	if err := s.db.DeleteUser(ctx, req.UserID); err != nil {
		log.ZError(ctx, "PermanentDeleteUser DeleteUser failed", err, "userID", req.UserID)
		return nil, err
	}
	log.ZInfo(ctx, "audit user permanently deleted", "operator", operatorID, "userID", req.UserID, "time", time.Now())
	return &pbuser.PermanentDeleteUserResp{}, nil
}

// MarkedDeleteUser marks an archived user as deleted. Relations/groups/conversations
// are cleaned up but messages are kept, and the userID becomes locked forever.
func (s *userServer) MarkedDeleteUser(ctx context.Context, req *pbuser.MarkedDeleteUserReq) (*pbuser.MarkedDeleteUserResp, error) {
	if err := authverify.CheckAdmin(ctx); err != nil {
		return nil, err
	}
	operatorID := mcontext.GetOpUserID(ctx)
	user, err := s.getNormalUser(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	if err := s.checkArchived(user, req.UserID); err != nil {
		return nil, err
	}

	if _, err := s.relationClient.CleanUserAllRelations(ctx, &pbrelation.CleanUserAllRelationsReq{UserID: req.UserID}); err != nil {
		log.ZError(ctx, "MarkedDeleteUser CleanUserAllRelations failed", err, "userID", req.UserID)
		return nil, err
	}
	if _, err := s.groupClient.QuitUserAllGroups(ctx, &pbgroup.QuitUserAllGroupsReq{UserID: req.UserID}); err != nil {
		log.ZError(ctx, "MarkedDeleteUser QuitUserAllGroups failed", err, "userID", req.UserID)
		return nil, err
	}
	if _, err := s.conversationClient.DeleteUserAllConversations(ctx, &pbconversation.DeleteUserAllConversationsReq{UserID: req.UserID}); err != nil {
		log.ZError(ctx, "MarkedDeleteUser DeleteUserAllConversations failed", err, "userID", req.UserID)
		return nil, err
	}
	if err := s.clearPushToken(ctx, req.UserID); err != nil {
		log.ZError(ctx, "MarkedDeleteUser clearPushToken failed", err, "userID", req.UserID)
		return nil, err
	}
	// keep messages, anonymize the account profile and lock the state as deleted
	if err := s.db.UpdateByMap(ctx, req.UserID, map[string]any{
		"status":              model.UserStatusDeleted,
		"nickname":            model.AnonymizedNickname,
		"face_url":            "",
		"ex":                  "",
		"global_recv_msg_opt": 2,
		"delete_user_id":      operatorID,
		"delete_time":         time.Now(),
	}); err != nil {
		log.ZError(ctx, "MarkedDeleteUser UpdateByMap failed", err, "userID", req.UserID)
		return nil, err
	}
	log.ZInfo(ctx, "audit user marked deleted", "operator", operatorID, "userID", req.UserID, "time", time.Now())
	return &pbuser.MarkedDeleteUserResp{}, nil
}

// GetArchivedUsers paginates archived users.
func (s *userServer) GetArchivedUsers(ctx context.Context, req *pbuser.GetArchivedUsersReq) (*pbuser.GetArchivedUsersResp, error) {
	if err := authverify.CheckAdmin(ctx); err != nil {
		return nil, err
	}
	total, users, err := s.db.PageByStatus(ctx, model.UserStatusArchived, req.Pagination)
	if err != nil {
		return nil, err
	}
	return &pbuser.GetArchivedUsersResp{Total: int32(total), Users: convert.UsersDB2Pb(users)}, nil
}

// GetDeletedUsers paginates marked-deleted users.
func (s *userServer) GetDeletedUsers(ctx context.Context, req *pbuser.GetDeletedUsersReq) (*pbuser.GetDeletedUsersResp, error) {
	if err := authverify.CheckAdmin(ctx); err != nil {
		return nil, err
	}
	total, users, err := s.db.PageByStatus(ctx, model.UserStatusDeleted, req.Pagination)
	if err != nil {
		return nil, err
	}
	return &pbuser.GetDeletedUsersResp{Total: int32(total), Users: convert.UsersDB2Pb(users)}, nil
}
