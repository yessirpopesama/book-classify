package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// IsAllowedBookFile 是否为支持分类的图书格式
func IsAllowedBookFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".txt" || ext == ".pdf" || ext == ".epub" || ext == ".mobi"
}

// GeneratePrompt 从source文件夹读取所有文件名，生成prompts提示器字符串
// 返回: prompts字符串, 文件相对路径列表, 错误
func GeneratePrompt(sourceDir string) (string, []string, error) {
	// 检查source目录是否存在
	if _, err := os.Stat(sourceDir); os.IsNotExist(err) {
		return "", nil, err
	}

	// 用于存储所有文件的相对路径
	var allFilePaths []string
	// 用于存储所有文件名（用于prompt）
	var allFileNames []string

	// 需要排除的文件列表
	excludedFiles := map[string]bool{
		"scan.bat":   true,
		"结果.txt":     true,
		"待进行分类.txt": true,
		".DS_Store":    true,
	}

	// 获取sourceDir的绝对路径，用于计算相对路径
	absSourceDir, err := filepath.Abs(sourceDir)
	if err != nil {
		return "", nil, fmt.Errorf("获取source目录绝对路径失败: %v", err)
	}

	// 遍历source目录下的所有文件（递归）
	err = filepath.Walk(absSourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 只收集文件，跳过目录
		if !info.IsDir() {
			fileName := info.Name()
			// 排除指定文件，仅保留支持的图书格式
			if !excludedFiles[fileName] && IsAllowedBookFile(fileName) {
				// 计算相对于sourceDir的相对路径
				relPath, err := filepath.Rel(absSourceDir, path)
				if err != nil {
					return err
				}
				allFilePaths = append(allFilePaths, relPath)
				allFileNames = append(allFileNames, fileName)
			}
		}

		return nil
	})

	if err != nil {
		return "", nil, err
	}

	// 如果没有找到任何文件，返回空
	if len(allFileNames) == 0 {
		return "", []string{}, nil
	}

	// 将所有文件名用换行符连接，最后添加"列表展示如何分类"
	prompts := strings.Join(allFileNames, "\n") + "\n列表展示如何分类\n请作为图书管理员，参照全国主要图书馆的中图法著录实践，使用中图法第五版（最新版）对以上书籍分类，并给出最优分类路径与完整类目层级"

	return prompts, allFilePaths, nil
}

// GenerateCatalog 生成目录.txt文件，包含source文件夹下的所有文件名
func GenerateCatalog(sourceDir, outputFile string) error {
	// 检查source目录是否存在
	if _, err := os.Stat(sourceDir); os.IsNotExist(err) {
		return fmt.Errorf("source目录不存在: %v", err)
	}

	// 用于存储所有文件名
	var allFileNames []string

	// 需要排除的文件列表
	excludedFiles := map[string]bool{
		"scan.bat":   true,
		"结果.txt":     true,
		"待进行分类.txt": true,
		".DS_Store":    true,
	}

	// 获取sourceDir的绝对路径，用于计算相对路径
	absSourceDir, err := filepath.Abs(sourceDir)
	if err != nil {
		return fmt.Errorf("获取source目录绝对路径失败: %v", err)
	}

	// 遍历source目录下的所有文件（递归）
	err = filepath.Walk(absSourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 只收集文件，跳过目录
		if !info.IsDir() {
			fileName := info.Name()
			// 排除指定文件，仅保留支持的图书格式
			if !excludedFiles[fileName] && IsAllowedBookFile(fileName) {
				allFileNames = append(allFileNames, fileName)
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("遍历source目录失败: %v", err)
	}

	// 创建或打开输出文件
	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("创建目录文件失败: %v", err)
	}
	defer file.Close()

	// 将文件名写入文件，每个文件名一行
	for _, fileName := range allFileNames {
		_, err := fmt.Fprintln(file, fileName)
		if err != nil {
			return fmt.Errorf("写入文件失败: %v", err)
		}
	}

	return nil
}
