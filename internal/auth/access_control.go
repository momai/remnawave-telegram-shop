package auth

import (
	"encoding/json"
	"os"
	"sync"
	"log/slog"
)

const (
	ACTIVATION_CODE = "00196400"
	ACCESS_FILE     = "./data/approved_users.json"
)

type AccessControl struct {
	mu           sync.RWMutex
	approvedUsers map[int64]bool
}

type ApprovedUsersData struct {
	Users []int64 `json:"users"`
}

func NewAccessControl() *AccessControl {
	ac := &AccessControl{
		approvedUsers: make(map[int64]bool),
	}
	ac.loadFromFile()
	return ac
}

func (ac *AccessControl) IsUserApproved(userID int64) bool {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.approvedUsers[userID]
}

func (ac *AccessControl) ApproveUser(userID int64) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	
	ac.approvedUsers[userID] = true
	return ac.saveToFile()
}

func (ac *AccessControl) GetApprovedUsers() []int64 {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	
	users := make([]int64, 0, len(ac.approvedUsers))
	for userID := range ac.approvedUsers {
		users = append(users, userID)
	}
	return users
}

func (ac *AccessControl) loadFromFile() {
	// Создаем папку data если её нет
	if err := os.MkdirAll("./data", 0755); err != nil {
		slog.Error("Error creating data directory", "error", err)
		return
	}

	data, err := os.ReadFile(ACCESS_FILE)
	if err != nil {
		if os.IsNotExist(err) {
			// Файл не существует, создаем пустой
			ac.saveToFile()
			return
		}
		slog.Error("Error reading access file", "error", err)
		return
	}

	var userData ApprovedUsersData
	if err := json.Unmarshal(data, &userData); err != nil {
		slog.Error("Error unmarshaling access file", "error", err)
		return
	}

	for _, userID := range userData.Users {
		ac.approvedUsers[userID] = true
	}
}

func (ac *AccessControl) saveToFile() error {
	users := make([]int64, 0, len(ac.approvedUsers))
	for userID := range ac.approvedUsers {
		users = append(users, userID)
	}

	userData := ApprovedUsersData{Users: users}
	
	data, err := json.MarshalIndent(userData, "", "  ")
	if err != nil {
		slog.Error("Error marshaling access data", "error", err)
		return err
	}

	if err := os.WriteFile(ACCESS_FILE, data, 0644); err != nil {
		slog.Error("Error writing access file", "error", err)
		return err
	}

	return nil
} 