package evilconfig

import (
	"os"

	"github.com/google/uuid"
)

const uuidFile = "device_id.txt"

func GetOrCreateDeviceID() (string, error) {
	// Try to read existing UUID
	if data, err := os.ReadFile(uuidFile); err == nil {
		return string(data), nil
	}

	// Generate new UUID
	newID := uuid.New().String()
	err := os.WriteFile(uuidFile, []byte(newID), 0644)
	return newID, err
}
