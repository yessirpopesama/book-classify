package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"book-distribute/service"
)

func main() {
	// 从config.yaml加载配置
	configPath := "config.yaml"
	config, err := service.LoadConfig(configPath)
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		fmt.Println("请确保config.yaml文件存在且包含deepseek_api_key配置")
		os.Exit(1)
	}

	apiKey := config.DeepSeekAPIKey

	// 设置目录路径
	sourceDir := "source"
	resultsDir := "results"

	// 检查source目录是否存在
	if _, err := os.Stat(sourceDir); os.IsNotExist(err) {
		fmt.Printf("错误: 找不到source目录 '%s'\n", sourceDir)
		fmt.Println("请确保source目录存在并包含要分类的图书文件")
		os.Exit(1)
	}

	// 创建DeepSeek客户端
	client := service.NewDeepSeekClient(apiKey,
		time.Duration(config.DeepSeekRequestTimeoutSeconds)*time.Second)

	// 获取绝对路径
	absSourceDir, err := filepath.Abs(sourceDir)
	if err != nil {
		fmt.Printf("获取source目录绝对路径失败: %v\n", err)
		os.Exit(1)
	}

	absResultsDir, err := filepath.Abs(resultsDir)
	if err != nil {
		fmt.Printf("获取results目录绝对路径失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("源目录: %s\n", absSourceDir)
	fmt.Printf("结果目录: %s\n", absResultsDir)
	fmt.Println()

	// 生成目录.txt文件
	catalogFile := "目录.txt"
	if err := service.GenerateCatalog(absSourceDir, catalogFile); err != nil {
		fmt.Printf("生成目录文件失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("已生成目录文件: %s\n", catalogFile)
	fmt.Println()

	// 执行分类和移动
	if err := service.ClassifyAndMove(absSourceDir, absResultsDir, client); err != nil {
		fmt.Printf("分类过程出错: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n所有操作完成！")
}
