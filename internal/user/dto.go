package user

import "time"

// 请求和响应单独定义，避免客户端提交数据库字段或收到密码哈希。
type userResponse struct {
	ID        uint64    `json:"id,string"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

func publicUser(user User) userResponse {
	return userResponse{
		ID:        user.ID,
		Username:  user.Username,
		CreatedAt: user.CreatedAt,
	}
}

type loginResponse struct {
	UserID             uint64 `json:"user_id,string"`
	Username           string `json:"user_name"`
	Token              string `json:"token"`
	Role               string `json:"role"`
	MustChangePassword bool   `json:"must_change_password,omitempty"`
}
