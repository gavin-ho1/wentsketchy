package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings/icons"
	"github.com/lucax88x/wentsketchy/internal/homedir"
	"gopkg.in/yaml.v2"
)

type Cfg struct {
	Left       []string `yaml:"left"`
	Center     []string `yaml:"center"`
	Right      []string `yaml:"right"`
	LeftNotch  []string `yaml:"left_notch"`
	RightNotch []string `yaml:"right_notch"`
	LogLevel   string   `yaml:"log_level"`
}

type ConfigData struct {
	Left       []string `yaml:"left"`
	Center     []string `yaml:"center"`
	Right      []string `yaml:"right"`
	LeftNotch  []string `yaml:"left_notch"`
	RightNotch []string `yaml:"right_notch"`
	LogLevel   string   `yaml:"log_level"`
	Icons      struct {
		Workspace map[string]string `yaml:"workspace"`
	} `yaml:"icons"`
}

func ReadYaml() (*Cfg, error) {
	var configData ConfigData

	dir, err := homedir.Get()

	if err != nil {
		//nolint:errorlint // no wrap
		return nil, fmt.Errorf("config: error getting home dir. %v", err)
	}

	yamlData, err := os.ReadFile(filepath.Join(dir, "config.yaml"))

	if err != nil {
		//nolint:errorlint // no wrap
		return nil, fmt.Errorf("config: could not read file. %v", err)
	}

	err = yaml.Unmarshal(yamlData, &configData)

	if err != nil {
		//nolint:errorlint // no wrap
		return nil, fmt.Errorf("config: could not unmarshal cfg. %v", err)
	}

	if configData.Icons.Workspace != nil {
		icons.Workspace = configData.Icons.Workspace
	}

	return &Cfg{
		Left:       configData.Left,
		Center:     configData.Center,
		Right:      configData.Right,
		LeftNotch:  configData.LeftNotch,
		RightNotch: configData.RightNotch,
		LogLevel:   configData.LogLevel,
	}, nil
}
