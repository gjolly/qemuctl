package network

import (
	"fmt"

	"github.com/gjolly/qemuctl/internal/commands"
	"github.com/gjolly/qemuctl/internal/config"
)

// List of tap devices per network/bridge
var bridges = make(map[string][]string)

func createBridge(name string) error {
	return commands.Run("ip", "link", "add", name, "type", "bridge")
}

// StartNetworks starts the network controler and configures the bridges
func StartNetworks(configs map[string]config.NetworkConfig) error {
	for name := range configs {
		err := createBridge("qemuctl" + name)
		if err != nil {
			return err
		}

		bridges[name] = make([]string, 0)
	}

	return nil
}

// NewTapDevice creates a new tap device for the given network
func NewTapDevice(networkName string) (string, error) {
	devices, exists := bridges[networkName]
	if !exists {
		return "", fmt.Errorf("undefined network '%v'", networkName)
	}
	tapNum := len(devices)
	name := fmt.Sprintf("qemuctl%vtap%v", networkName, tapNum)

	return name, commands.Run("ip", "tuntap", "add", "dev", name, "mode", "tap")
}

// TODO add cleanup function
