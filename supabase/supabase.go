package supabase

import (
	"fmt"
	"log"
	"time"

	"github.com/supabase-community/supabase-go"
)

const supabaseUrl = "https://jeolgufjejxygjuyifbx.supabase.co"
const supabaseAnonKey = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJzdXBhYmFzZSIsInJlZiI6Implb2xndWZqZWp4eWdqdXlpZmJ4Iiwicm9sZSI6ImFub24iLCJpYXQiOjE3NDYxNjYwMjIsImV4cCI6MjA2MTc0MjAyMn0.2cobJDIqTGROGdlLRsATf10FRSteYj1RqV4mKW-CDV4"

var client *supabase.Client

// Initialize Supabase client
func InitSupabase() {
	var err error
	options := &supabase.ClientOptions{} // Provide appropriate options if needed
	client, err = supabase.NewClient(supabaseUrl, supabaseAnonKey, options)
	if err != nil {
		log.Fatalf("Error initializing Supabase client: %v", err)
	}
}

// Send data to Supabase
func SendToSupabase(deviceID string, logType string, data map[string]string) error {
	// Initialize Supabase client if not done already
	if client == nil {
		InitSupabase()
	}
	// Create timestamp for the current update
	currentTime := time.Now().UTC().Format(time.RFC3339)

	devicePayload := map[string]interface{}{
		"id":        deviceID,
		"name":      "MyDeviceName",
		"last_seen": currentTime,
	}

	// Try to upsert instead of insert - this will update if exists or insert if not
	_, _, err := client.From("devices").Upsert(devicePayload, "", "", "").Execute()
	if err != nil {
		return fmt.Errorf("failed to register/update device: %v", err)
	}

	payload := map[string]interface{}{
		"device_id":  deviceID,
		"type":       logType,
		"data":       data,
		"updated_at": currentTime,
	}

	// First try to update existing record
	_, _, err = client.From("collect_wifi").
		Update(payload, "", "").
		Eq("device_id", deviceID).
		Execute()

	if err != nil {
		// If update fails, try to insert
		payload["created_at"] = currentTime // Add created_at for new records
		_, _, err = client.From("collect_wifi").
			Insert(payload, false, "", "", "").
			Execute()
		if err != nil {
			return fmt.Errorf("failed to insert data in Supabase: %v", err)
		}
	}

	return nil
}
