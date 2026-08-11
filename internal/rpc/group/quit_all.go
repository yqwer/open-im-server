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
//   - v1.0.0: 新增 QuitUserAllGroups，用户删除机制中退出全部群组/解散自有群
// ----------------------------------------------------------------------------

package group

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	pbgroup "github.com/openimsdk/protocol/group"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/utils/datautil"
)

// QuitUserAllGroups makes a user leave all groups. Groups owned by the user are
// dismissed directly because the owner account is being deleted. Group requests
// related to the user are cleaned up as well.
func (s *groupServer) QuitUserAllGroups(ctx context.Context, req *pbgroup.QuitUserAllGroupsReq) (*pbgroup.QuitUserAllGroupsResp, error) {
	if err := authverify.CheckAdmin(ctx); err != nil {
		return nil, err
	}
	userID := req.UserID
	log.ZInfo(ctx, "QuitUserAllGroups", "userID", userID)

	// 1. groups owned by the user are dismissed (owner account is gone)
	ownerGroupIDs, err := s.db.FindUserManagedGroupID(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, groupID := range ownerGroupIDs {
		if err := s.db.DismissGroup(ctx, groupID, true); err != nil {
			return nil, err
		}
	}

	// 2. leave the remaining groups
	joinedGroupIDs, err := s.db.FindJoinGroupID(ctx, userID)
	if err != nil {
		return nil, err
	}
	remaining := datautil.SliceSub(joinedGroupIDs, ownerGroupIDs)
	for _, groupID := range remaining {
		if err := s.db.DeleteGroupMember(ctx, groupID, []string{userID}); err != nil {
			return nil, err
		}
	}

	// 3. clean group requests related to the user
	if err := s.db.DeleteGroupRequestByUser(ctx, userID); err != nil {
		return nil, err
	}

	return &pbgroup.QuitUserAllGroupsResp{}, nil
}
