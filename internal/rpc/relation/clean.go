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
//   - v1.0.0: 新增 CleanUserAllRelations，用户删除机制中清理全部好友/申请/黑名单
// ----------------------------------------------------------------------------

package relation

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/protocol/relation"
	"github.com/openimsdk/tools/log"
)

// CleanUserAllRelations removes all relations of a user: friends (bidirectional),
// friend requests and blacks (bidirectional). Used by the user deletion mechanism.
func (s *friendServer) CleanUserAllRelations(ctx context.Context, req *relation.CleanUserAllRelationsReq) (*relation.CleanUserAllRelationsResp, error) {
	if err := authverify.CheckAdmin(ctx); err != nil {
		return nil, err
	}
	userID := req.UserID
	log.ZInfo(ctx, "CleanUserAllRelations", "userID", userID)

	// 1. friends owned by the user
	if err := s.db.DeleteOwnerFriendAll(ctx, userID); err != nil {
		return nil, err
	}

	// 2. reversal friends: users who added the deleted user as their friend
	owners, err := s.db.FindFriendUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, owner := range owners {
		if err := s.db.Delete(ctx, owner, []string{userID}); err != nil {
			return nil, err
		}
	}

	// 3. friend requests related to the user
	if err := s.db.DeleteFriendRequestAllByUser(ctx, userID); err != nil {
		return nil, err
	}

	// 4. blacks owned by the user
	if err := s.blackDatabase.DeleteOwnerBlackAll(ctx, userID); err != nil {
		return nil, err
	}

	// 5. blacks pointing at the user
	if err := s.blackDatabase.DeleteBlackAllByBlockUserID(ctx, userID); err != nil {
		return nil, err
	}

	return &relation.CleanUserAllRelationsResp{}, nil
}
