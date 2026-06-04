package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// analysisResultItem 单本书的分析结果
type analysisResultItem struct {
	fileName      string
	analysis      *BookAnalysis
	err           error
	pendingReason string // 中图法校验未通过原因
	officialPath  string // 索引中的标准路径（供人工参考）
}

// ClassifyAndMove 分类并移动文件
// 读取每本书正文第一页（约1000字），分析作者、国籍、分类号及类目层级，输出分类号、书籍作者、书籍国籍
func ClassifyAndMove(sourceDir, resultsDir string, client *DeepSeekClient, clcIndexFile string) error {
	// 生成prompts（获取文件列表）
	_, filePaths, err := GeneratePrompt(sourceDir)
	if err != nil {
		return fmt.Errorf("生成prompt失败: %v", err)
	}

	if len(filePaths) == 0 {
		fmt.Println("未找到任何文件")
		return nil
	}

	fmt.Printf("找到 %d 个文件，开始分析（正文第一页约1000字）...\n", len(filePaths))

	// 创建results目录
	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		return fmt.Errorf("创建results目录失败: %v", err)
	}

	clcIndex, clcErr := LoadCLCIndex(clcIndexFile)
	if clcErr != nil {
		fmt.Printf("警告: %v，将不进行路径校验\n", clcErr)
	}

	// 收集分析结果用于输出
	var successResults []analysisResultItem
	var pendingResults []analysisResultItem

	successCount := 0
	pendingCount := 0
	failCount := 0

	for _, relPath := range filePaths {
		fileName := filepath.Base(relPath)
		fullPath := filepath.Join(sourceDir, relPath)
		fmt.Printf("\n处理文件: %s\n", fullPath)

		// 读取内容（正文第一页，约1000字）
		bookContent, ok, err := ReadBookFirstPages(fullPath, 1)
		if err != nil {
			fmt.Printf("  无法读取正文（%s），移至未识别文件夹\n", err.Error())
			moveToUnrecognized(sourceDir, relPath, resultsDir, fileName, &successCount, &failCount)
			pendingResults = append(pendingResults, analysisResultItem{fileName: fileName, err: err})
			continue
		}
		if !ok || bookContent == "" {
			fmt.Printf("  无正文内容，移至未识别文件夹\n")
			moveToUnrecognized(sourceDir, relPath, resultsDir, fileName, &successCount, &failCount)
			pendingResults = append(pendingResults, analysisResultItem{fileName: fileName, err: fmt.Errorf("未读取到文档内容")})
			continue
		}
		fmt.Printf("  已读取正文（约%d字）\n", len([]rune(bookContent)))

		// 调用API分析
		analysis, err := client.AnalyzeBook(fileName, bookContent)
		if err != nil {
			fmt.Printf("  分析失败: %v\n", err)
			pendingResults = append(pendingResults, analysisResultItem{fileName: fileName, err: err})
			moveToUnrecognized(sourceDir, relPath, resultsDir, fileName, &successCount, &failCount)
			continue
		}

		// 输出分析结果
		fmt.Printf("  分类号: %s\n", analysis.Classification)
		if analysis.LibraryReference != "" {
			fmt.Printf("  图书馆参考: %s\n", analysis.LibraryReference)
		}
		if analysis.ClassificationPath != "" {
			fmt.Printf("  最优分类路径: %s\n", analysis.ClassificationPath)
		}
		printCategoryHierarchy(analysis.CategoryLevels)
		fmt.Printf("  书籍作者: %s\n", analysis.Author)
		fmt.Printf("  书籍国籍: %s\n", analysis.Nationality)
		// 验证分类号
		classification := strings.TrimSpace(analysis.Classification)
		if classification == "" {
			fmt.Printf("  分类号为空，移至未识别文件夹\n")
			moveToUnrecognized(sourceDir, relPath, resultsDir, fileName, &successCount, &failCount)
			pendingResults = append(pendingResults, analysisResultItem{
				fileName: fileName, analysis: analysis, pendingReason: "分类号为空",
			})
			continue
		}

		// 中图法索引校验：分类号须在索引中，且与路径末级类号一致（若有路径）
		if clcIndex != nil {
			v := clcIndex.ValidateAnalysis(analysis)
			if !v.OK {
				fmt.Printf("  中图法校验未通过: %s\n", v.Reason)
				if v.OfficialPath != "" {
					fmt.Printf("  标准路径参考: %s\n", v.OfficialPath)
				}
				moveToPendingClassify(sourceDir, relPath, resultsDir, fileName)
				pendingResults = append(pendingResults, analysisResultItem{
					fileName:      fileName,
					analysis:      analysis,
					pendingReason: v.Reason,
					officialPath:  v.OfficialPath,
				})
				pendingCount++
				continue
			}
			if strings.TrimSpace(analysis.Classification) == "" {
				analysis.Classification = FinalCodeFromAnalysis(analysis)
			}
			clcIndex.ApplyOfficialPath(analysis, v)
			fmt.Printf("  中图法校验通过（分类号与路径末级一致）\n")
			if analysis.ClassificationPath != "" {
				fmt.Printf("  最优分类路径: %s\n", analysis.ClassificationPath)
			}
		}

		successResults = append(successResults, analysisResultItem{fileName: fileName, analysis: analysis})

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
	outputAnalysisResults(successResults, filepath.Join(resultsDir, "结果.txt"))
	outputPendingResults(pendingResults, filepath.Join(resultsDir, PendingClassifyFile))

	fmt.Printf("\n分类完成！入库成功: %d, 待人工分类: %d, 未识别/失败: %d\n", successCount, pendingCount, failCount)
	return nil
}

// printCategoryHierarchy 在控制台打印完整类目层级
func printCategoryHierarchy(levels []string) {
	if len(levels) == 0 {
		return
	}
	fmt.Println("  完整类目层级:")
	for i, level := range levels {
		fmt.Printf("    %d. %s\n", i+1, level)
	}
}

// outputAnalysisResults 将分析结果输出到指定文件
func outputAnalysisResults(results []analysisResultItem, outputPath string) {
	if outputPath == "" {
		outputPath = "结果.txt"
	}
	outputFile := outputPath
	f, err := os.Create(outputFile)
	if err != nil {
		fmt.Printf("无法创建结果文件 %s: %v\n", outputFile, err)
		return
	}
	defer f.Close()

	// 表头
	fmt.Fprintf(f, "%-50s\t%-20s\t%-60s\t%-30s\t%-20s\n", "书名", "分类号", "最优分类路径", "书籍作者", "书籍国籍")
	fmt.Fprintf(f, "%s\n", strings.Repeat("-", 180))

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
		path := r.analysis.ClassificationPath
		if path == "" && len(r.analysis.CategoryLevels) > 0 {
			path = strings.Join(r.analysis.CategoryLevels, " > ")
		}
		fmt.Fprintf(f, "%-50s\t%-20s\t%-60s\t%-30s\t%-20s\n",
			r.fileName,
			r.analysis.Classification,
			path,
			r.analysis.Author,
			r.analysis.Nationality,
		)
	}

	fmt.Printf("\n分析结果已保存到: %s\n", outputFile)
}

// outputPendingResults 将未通过中图法校验的结果写入待进行分类.txt
func outputPendingResults(results []analysisResultItem, outputPath string) {
	if len(results) == 0 {
		return
	}
	f, err := os.Create(outputPath)
	if err != nil {
		fmt.Printf("无法创建待分类文件 %s: %v\n", outputPath, err)
		return
	}
	defer f.Close()

	fmt.Fprintln(f, "以下图书的中图法分类路径未通过本地索引校验，请对照 data/clc/clc_index.json 手工完成分类。")
	fmt.Fprintf(f, "%-50s\t%-20s\t%-50s\t%-30s\t%-20s\t%s\n",
		"书名", "分类号", "模型分类路径", "书籍作者", "书籍国籍", "校验说明/标准路径参考")
	fmt.Fprintln(f, strings.Repeat("-", 200))

	for _, r := range results {
		if r.err != nil && r.analysis == nil {
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
		path := r.analysis.ClassificationPath
		if path == "" && len(r.analysis.CategoryLevels) > 0 {
			path = strings.Join(r.analysis.CategoryLevels, " > ")
		}
		note := r.pendingReason
		if r.officialPath != "" {
			if note != "" {
				note += "；"
			}
			note += "标准路径: " + r.officialPath
		}
		fmt.Fprintf(f, "%-50s\t%-20s\t%-50s\t%-30s\t%-20s\t%s\n",
			r.fileName,
			r.analysis.Classification,
			path,
			r.analysis.Author,
			r.analysis.Nationality,
			note,
		)
	}
	fmt.Printf("待人工分类列表已保存到: %s\n", outputPath)
}

// moveToPendingClassify 将文件移至待分类目录
func moveToPendingClassify(sourceDir, relPath, resultsDir, fileName string) {
	dir := filepath.Join(resultsDir, PendingClassifyDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Printf("  创建待分类目录失败: %v\n", err)
		return
	}
	srcPath := filepath.Join(sourceDir, relPath)
	destPath := filepath.Join(dir, fileName)
	if err := moveFile(srcPath, destPath); err != nil {
		fmt.Printf("  移动至待分类目录失败: %v\n", err)
		return
	}
	fmt.Printf("  已移至: %s/（待人工分类）\n", PendingClassifyDir)
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
