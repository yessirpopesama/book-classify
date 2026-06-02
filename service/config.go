package service

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 配置结构
type Config struct {
	DeepSeekAPIKey string `yaml:"deepseek_api_key"`
}

// LoadConfig 从config.yaml加载配置
func LoadConfig(configPath string) (*Config, error) {
	// 检查配置文件是否存在
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("配置文件不存在: %s", configPath)
	}

	// 读取配置文件
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %v", err)
	}

	// 解析YAML
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %v", err)
	}

	// 检查API Key是否为空
	if config.DeepSeekAPIKey == "" {
		return nil, fmt.Errorf("配置文件中deepseek_api_key为空")
	}

	return &config, nil
}
