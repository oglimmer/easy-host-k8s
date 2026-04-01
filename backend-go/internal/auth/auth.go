package auth

import (
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	Username string
	Role     string // "USER" or "ACTUATOR"
}

type UserStore struct {
	users map[string]userEntry
}

type userEntry struct {
	hashedPassword []byte
	role           string
}

func NewUserStore(adminUser, adminPass, actuatorUser, actuatorPass string) *UserStore {
	s := &UserStore{users: make(map[string]userEntry)}
	adminHash, _ := bcrypt.GenerateFromPassword([]byte(adminPass), bcrypt.DefaultCost)
	actuatorHash, _ := bcrypt.GenerateFromPassword([]byte(actuatorPass), bcrypt.DefaultCost)
	s.users[adminUser] = userEntry{hashedPassword: adminHash, role: "USER"}
	s.users[actuatorUser] = userEntry{hashedPassword: actuatorHash, role: "ACTUATOR"}
	return s
}

func (s *UserStore) Authenticate(username, password string) *User {
	entry, ok := s.users[username]
	if !ok {
		return nil
	}
	if err := bcrypt.CompareHashAndPassword(entry.hashedPassword, []byte(password)); err != nil {
		return nil
	}
	return &User{Username: username, Role: entry.role}
}
