package config

import (
	"github.com/spf13/viper"
)

// UserConfig defines the configuration of a user
type UserConfig struct {
	Password    string
	SSHImportID []string
	SSHKeys     []string
}

// NetworkConfig defines the configuration of a network
type NetworkConfig struct {
	Type string
}

// MachineConfig defines the configuration of a VM
type MachineConfig struct {
	Image    string
	UEFI     bool
	Snapshot bool
	Memory   string
	Network  string
	Users    map[string]UserConfig
	Arch     string
	Suite    string
}

// Config defines the configuration of
// the VMs
type Config struct {
	Machines map[string]MachineConfig
	Network  map[string]NetworkConfig
}

// GetConfig parses the config files
func GetConfig(path string) (*Config, error) {
	viper.SetConfigName(path)
	conf := new(Config)
	err := viper.Unmarshal(conf)

	if err != nil {
		return nil, err
	}

	return conf, nil
}
