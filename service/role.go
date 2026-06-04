package service

import (
	_ "embed"
	"fmt"
	"os"
	"strings"
)

// defaultClassifierRole 内置的默认“图书馆分类员”角色提示词，作为兜底。
//
//go:embed classifier_role.txt
var defaultClassifierRole string

// DefaultClassifierRoleFile 运行时可编辑的角色提示词文件（相对工作目录）。
const DefaultClassifierRoleFile = "classifier_role.txt"

// LoadClassifierRole 加载“图书馆分类员”角色提示词。
// 优先读取外部文件 path（为空时用 DefaultClassifierRoleFile）；
// 若文件不存在或为空，则把内置默认角色写出到该路径（便于用户直接编辑、持久保存），并返回默认角色。
func LoadClassifierRole(path string) string {
	if strings.TrimSpace(path) == "" {
		path = DefaultClassifierRoleFile
	}

	if data, err := os.ReadFile(path); err == nil {
		if strings.TrimSpace(string(data)) != "" {
			fmt.Printf("已加载角色提示词: %s\n", path)
			return string(data)
		}
	}

	// 文件缺失或为空：写出默认角色，供后续编辑
	if err := os.WriteFile(path, []byte(defaultClassifierRole), 0644); err != nil {
		fmt.Printf("提示: 无法写出默认角色提示词文件 %s: %v（将使用内置默认角色）\n", path, err)
	} else {
		fmt.Printf("已生成可编辑的角色提示词文件: %s\n", path)
	}
	return defaultClassifierRole
}
