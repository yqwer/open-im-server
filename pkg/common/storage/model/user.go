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
// 作者:      yqwer / DeepSeek-V4
// 最后修订人: Composer (Cursor Agent)
// 变更说明:
//   - v1.0.0: 新增用户状态字段(Status)及状态常量，支持用户删除机制
//   - v1.1.0: 明确 Status 默认值为 UserStatusNormal(0)；缺字段按 normal 处理
// ----------------------------------------------------------------------------

package model

import (
	"time"
)

// User account states for the user deletion mechanism.
const (
	// UserStatusNormal normal account, can use all features.
	UserStatusNormal int32 = 0
	// UserStatusArchived archived account, data fully kept but account unusable.
	UserStatusArchived int32 = 1
	// UserStatusDeleted marked-deleted account, record kept but terminal state.
	UserStatusDeleted int32 = 2
)

// Placeholder values used when anonymizing a permanently deleted user.
const (
	// AnonymizedSenderID is the send_id used to replace a deleted user's id in group messages.
	AnonymizedSenderID = "deleted_user"
	// AnonymizedNickname is the nickname shown for a deleted user.
	AnonymizedNickname = "已注销用户"
)

type User struct {
	UserID           string    `bson:"user_id"`
	Nickname         string    `bson:"nickname"`
	FaceURL          string    `bson:"face_url"`
	Ex               string    `bson:"ex"`
	AppMangerLevel   int32     `bson:"app_manger_level"`
	GlobalRecvMsgOpt int32     `bson:"global_recv_msg_opt"`
	CreateTime       time.Time `bson:"create_time"`
	// Status is the account state, see UserStatusNormal/Archived/Deleted.
	// Default is UserStatusNormal (0). Legacy documents without this field
	// must be treated as normal (Go zero-value and DB migration both map to 0).
	Status int32 `bson:"status"`
	// DeleteUserID is the operator userID who archived/deleted this account.
	DeleteUserID string `bson:"delete_user_id"`
	// DeleteReason is the reason given by the operator.
	DeleteReason string `bson:"delete_reason"`
	// DeleteTime is the time when the account was archived/deleted.
	DeleteTime time.Time `bson:"delete_time"`
}

func (u *User) GetNickname() string {
	return u.Nickname
}

func (u *User) GetFaceURL() string {
	return u.FaceURL
}

func (u *User) GetUserID() string {
	return u.UserID
}

func (u *User) GetEx() string {
	return u.Ex
}

// IsNormal reports whether the account is in the normal state.
func (u *User) IsNormal() bool {
	return u.Status == UserStatusNormal
}
