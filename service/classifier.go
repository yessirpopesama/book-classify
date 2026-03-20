package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ClassifyAndMove 分类并移动文件
// 读取每本书前3页，分析作者、国籍，输出分类号、书籍作者、书籍国籍
func ClassifyAndMove(sourceDir, resultsDir string, client *DeepSeekClient) error {
	// 生成prompts（获取文件列表）
	_, filePaths, err := GeneratePrompt(sourceDir)
	if err != nil {
		return fmt.Errorf("生成prompt失败: %v", err)
	}

	if len(filePaths) == 0 {
		fmt.Println("未找到任何文件")
		return nil
	}

	fmt.Printf("找到 %d 个文件，开始分析（读取前3页）...\n", len(filePaths))

	// 创建results目录
	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		return fmt.Errorf("创建results目录失败: %v", err)
	}

	// 收集分析结果用于输出
	var analysisResults []struct {
		fileName string
		analysis *BookAnalysis
		err      error
	}

	successCount := 0
	failCount := 0

	for _, relPath := range filePaths {
		fileName := filepath.Base(relPath)
		fullPath := filepath.Join(sourceDir, relPath)
		fmt.Printf("\n处理文件: %s\n", fullPath)

		// 读取内容（txt、pdf、epub 均为前3页）
		bookContent, ok, err := ReadBookFirstPages(fullPath, 3)
		if err != nil {
			fmt.Printf("  无法读取正文（%s），移至未识别文件夹\n", err.Error())
			moveToUnrecognized(sourceDir, relPath, resultsDir, fileName, &successCount, &failCount)
			analysisResults = append(analysisResults, struct {
				fileName string
				analysis *BookAnalysis
				err      error
			}{fileName, nil, err})
			continue
		}
		if !ok || bookContent == "" {
			fmt.Printf("  无正文内容，移至未识别文件夹\n")
			moveToUnrecognized(sourceDir, relPath, resultsDir, fileName, &successCount, &failCount)
			analysisResults = append(analysisResults, struct {
				fileName string
				analysis *BookAnalysis
				err      error
			}{fileName, nil, fmt.Errorf("未读取到文档内容")})
			continue
		}
		fmt.Printf("  已读取正文（约%d字）\n", len([]rune(bookContent)))

		// 调用API分析
		analysis, err := client.AnalyzeBook(fileName, bookContent)
		if err != nil {
			fmt.Printf("  分析失败: %v\n", err)
			analysisResults = append(analysisResults, struct {
				fileName string
				analysis *BookAnalysis
				err      error
			}{fileName, nil, err})
			moveToUnrecognized(sourceDir, relPath, resultsDir, fileName, &successCount, &failCount)
			continue
		}

		// 输出三项结果
		fmt.Printf("  分类号: %s\n", analysis.Classification)
		fmt.Printf("  书籍作者: %s\n", analysis.Author)
		fmt.Printf("  书籍国籍: %s\n", analysis.Nationality)
		analysisResults = append(analysisResults, struct {
			fileName string
			analysis *BookAnalysis
			err      error
		}{fileName, analysis, nil})

		// 验证分类号
		classification := strings.TrimSpace(analysis.Classification)
		if classification == "" {
			fmt.Printf("  分类号为空，移至未识别文件夹\n")
			moveToUnrecognized(sourceDir, relPath, resultsDir, fileName, &successCount, &failCount)
			continue
		}

		// 创建分类目录并移动
		classDir := filepath.Join(resultsDir, classification)
		if err := os.MkdirAll(classDir, 0755); err != nil {
			fmt.Printf("  创建分类目录失败: %v\n", err)
			failCount++
			continue
		}

		srcPath := filepath.Join(sourceDir, relPath)
		destPath := filepath.Join(classDir, fileName)
		if err := moveFile(srcPath, destPath); err != nil {
			fmt.Printf("  移动文件失败: %v\n", err)
			failCount++
			continue
		}

		fmt.Printf("  已移动到: %s/\n", classification)
		successCount++
	}

	// 输出汇总结果到文件
	outputAnalysisResults(analysisResults)

	fmt.Printf("\n分类完成！成功: %d, 失败: %d\n", successCount, failCount)
	return nil
}

// outputAnalysisResults 将分析结果输出到 结果.txt
func outputAnalysisResults(results []struct {
	fileName string
	analysis *BookAnalysis
	err      error
}) {
	outputFile := "结果.txt"
	f, err := os.Create(outputFile)
	if err != nil {
		fmt.Printf("无法创建结果文件 %s: %v\n", outputFile, err)
		return
	}
	defer f.Close()

	// 表头
	fmt.Fprintf(f, "%-50s\t%-20s\t%-30s\t%-20s\n", "书名", "分类号", "书籍作者", "书籍国籍")
	fmt.Fprintf(f, "%s\n", strings.Repeat("-", 120))

	for _, r := range results {
		if r.err != nil {
			msg := r.err.Error()
			if msg != "未读取到文档内容" {
				msg = "分析失败: " + msg
			}
			fmt.Fprintf(f, "%-50s\t%s\n", r.fileName, msg)
			continue
		}
		if r.analysis == nil {
			fmt.Fprintf(f, "%-50s\t%s\n", r.fileName, "无结果")
			continue
		}
		fmt.Fprintf(f, "%-50s\t%-20s\t%-30s\t%-20s\n",
			r.fileName,
			r.analysis.Classification,
			r.analysis.Author,
			r.analysis.Nationality,
		)
	}

	fmt.Printf("\n分析结果已保存到: %s\n", outputFile)
}

// UnrecognizedDir 未识别文件夹名
const UnrecognizedDir = "未识别"

// moveToUnrecognized 将文件移至未识别文件夹
func moveToUnrecognized(sourceDir, relPath, resultsDir, fileName string, successCount, failCount *int) {
	dir := filepath.Join(resultsDir, UnrecognizedDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Printf("  创建未识别目录失败: %v\n", err)
		*failCount++
		return
	}
	srcPath := filepath.Join(sourceDir, relPath)
	destPath := filepath.Join(dir, fileName)
	if err := moveFile(srcPath, destPath); err != nil {
		fmt.Printf("  移动文件失败: %v\n", err)
		*failCount++
		return
	}
	fmt.Printf("  已移动到: %s/\n", UnrecognizedDir)
	*successCount++
	*failCount++
}

// moveFile 移动文件
func moveFile(src, dest string) error {
	// 如果目标文件已存在，添加序号
	if _, err := os.Stat(dest); err == nil {
		ext := filepath.Ext(dest)
		name := strings.TrimSuffix(dest, ext)
		counter := 1
		for {
			newDest := fmt.Sprintf("%s_%d%s", name, counter, ext)
			if _, err := os.Stat(newDest); os.IsNotExist(err) {
				dest = newDest
				break
			}
			counter++
		}
	}

	return os.Rename(src, dest)
}
