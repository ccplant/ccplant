package entities

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var localUsernamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)

var (
	ErrLocalUserNotFound      = errors.New("local user not found")
	ErrLocalUserAlreadyExists = errors.New("local user already exists")
)

type LocalUser struct {
	ID          UserID     `json:"id"`
	Username    string     `json:"username"`
	DisplayName string     `json:"display_name"`
	Email       string     `json:"email,omitempty"`
	Role        Role       `json:"role"`
	Status      UserStatus `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	CreatedBy   UserID     `json:"created_by"`
}

func NewLocalUser(username, displayName, email string, role Role, createdBy UserID) (*LocalUser, error) {
	if displayName == "" {
		displayName = username
	}
	u := &LocalUser{ID: UserID("local:" + username), Username: username, DisplayName: displayName,
		Email: email, Role: role, Status: UserStatusActive, CreatedAt: time.Now().UTC(), CreatedBy: createdBy}
	if err := u.Validate(); err != nil {
		return nil, err
	}
	return u, nil
}

func (u *LocalUser) Validate() error {
	if u == nil || !localUsernamePattern.MatchString(u.Username) {
		return errors.New("username must match [a-z][a-z0-9_-]{0,62}")
	}
	if u.ID != UserID("local:"+u.Username) {
		return errors.New("invalid local user id")
	}
	if n := len([]rune(u.DisplayName)); n < 1 || n > 128 || strings.ContainsAny(u.DisplayName, "\r\n\x00") {
		return errors.New("display_name must be 1..128 characters without control characters")
	}
	if u.Email != "" && (!strings.Contains(u.Email, "@") || strings.ContainsAny(u.Email, "\r\n")) {
		return errors.New("invalid email")
	}
	if u.Role != RoleUser && u.Role != RoleAdmin {
		return errors.New("role must be user or admin")
	}
	if u.Status != UserStatusActive {
		return errors.New("only active local users are supported")
	}
	if u.CreatedAt.IsZero() || u.CreatedBy == "" {
		return errors.New("creation metadata is required")
	}
	return nil
}

func (u *LocalUser) ToUser(permissions []Permission) *User {
	result := NewUser(u.ID, UserTypeRegular, u.Username)
	result.SetEmail(u.Email)
	result.displayName = &u.DisplayName
	result.SetPermissions(permissions)
	if u.Role == RoleAdmin {
		for _, p := range permissions {
			if p == PermissionAdmin {
				_ = result.SetRoles([]Role{RoleAdmin})
				break
			}
		}
	}
	return result
}
