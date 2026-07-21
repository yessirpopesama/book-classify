package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"book-distribute/service"
)

const (
	uploadDir       = "data/uploads"
	resultsDir      = "data/results"
	repairUploadDir = "data/repair_uploads"
	repairResultsDir = "data/repair_results"
	port            = "8080"
	frontendPort    = "8887"
)

const (
	TaskTypeClassify = "classify"
	TaskTypeRepair   = "repair"
)

var (
	config     *service.Config
	client     *service.DeepSeekClient
	taskMu     sync.Mutex
	taskStatus = make(map[string]*TaskStatus)
)

type TaskStatus struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"` // classify, repair
	Status    string    `json:"status"` // pending, processing, completed, failed
	Message   string    `json:"message"`
	Total     int       `json:"total,omitempty"`
	Current   int       `json:"current,omitempty"`
	Progress  int       `json:"progress,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func updateTaskProgress(taskID string, done, total int, fileName, action string) {
	taskMu.Lock()
	defer taskMu.Unlock()
	ts := taskStatus[taskID]
	if ts == nil {
		return
	}
	ts.Total = total
	ts.Current = done
	if total > 0 {
		ts.Progress = done * 100 / total
	}
	if fileName != "" {
		ts.Message = fmt.Sprintf("正在%s (%d/%d): %s", action, done+1, total, fileName)
	}
}

func main() {
	var err error
	config, err = service.LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	role := service.LoadClassifierRole(config.ClassifierPromptFile)
	client = service.NewDeepSeekClient(config.DeepSeekAPIKey, role)

	os.MkdirAll(uploadDir, 0755)
	os.MkdirAll(resultsDir, 0755)
	os.MkdirAll(repairUploadDir, 0755)
	os.MkdirAll(repairResultsDir, 0755)

	http.HandleFunc("/api/upload", cors(handleUpload))
	http.HandleFunc("/api/classify", cors(handleClassify))
	http.HandleFunc("/api/repair/upload", cors(handleRepairUpload))
	http.HandleFunc("/api/repair/results/", cors(handleRepairResults))
	http.HandleFunc("/api/repair", cors(handleRepair))
	http.HandleFunc("/api/results/", cors(handleResults))
	http.HandleFunc("/api/status/", cors(handleStatus))
	http.HandleFunc("/api/download/", cors(handleDownload))
	http.HandleFunc("/api/clear", cors(handleClear))

	log.Printf("后端服务启动: http://localhost:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func cors(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next(w, r)
	}
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	taskID := fmt.Sprintf("%d", time.Now().UnixNano())
	taskDir := filepath.Join(uploadDir, taskID)
	os.MkdirAll(taskDir, 0755)

	if err := r.ParseMultipartForm(100 << 20); err != nil { // 100MB
		jsonError(w, "解析表单失败", http.StatusBadRequest)
		return
	}

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		jsonError(w, "请选择要上传的文件", http.StatusBadRequest)
		return
	}

	uploaded := 0
	for _, fh := range files {
		if !service.IsAllowedBookFile(fh.Filename) {
			continue
		}
		rel := filepath.Clean(filepath.FromSlash(fh.Filename))
		if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			continue
		}
		f, err := fh.Open()
		if err != nil {
			continue
		}
		dst := filepath.Join(taskDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			f.Close()
			continue
		}
		out, err := os.Create(dst)
		if err != nil {
			f.Close()
			continue
		}
		io.Copy(out, f)
		f.Close()
		out.Close()
		uploaded++
	}

	if uploaded == 0 {
		os.RemoveAll(taskDir)
		jsonError(w, "未找到符合条件的图书文件（支持 .txt、.pdf、.epub、.mobi）", http.StatusBadRequest)
		return
	}

	taskMu.Lock()
	taskStatus[taskID] = &TaskStatus{
		ID: taskID, Type: TaskTypeClassify, Status: "pending", Message: "上传完成",
		Total: uploaded, Current: 0, Progress: 0,
		CreatedAt: time.Now(),
	}
	taskMu.Unlock()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"task_id": taskID,
		"count":   uploaded,
	})
}

func handleClassify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		TaskID string `json:"task_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TaskID == "" {
		jsonError(w, "缺少 task_id", http.StatusBadRequest)
		return
	}

	taskDir := filepath.Join(uploadDir, req.TaskID)
	taskResultsDir := filepath.Join(resultsDir, req.TaskID)

	if _, err := os.Stat(taskDir); os.IsNotExist(err) {
		jsonError(w, "任务不存在", http.StatusNotFound)
		return
	}

	taskMu.Lock()
	taskStatus[req.TaskID].Status = "processing"
	taskStatus[req.TaskID].Message = "正在准备分类..."
	taskStatus[req.TaskID].Progress = 0
	taskMu.Unlock()

	taskID := req.TaskID
	go func() {
		os.MkdirAll(taskResultsDir, 0755)
		err := service.ClassifyAndMove(taskDir, taskResultsDir, client, config.CLCIndexFile, func(done, total int, fileName string) {
			updateTaskProgress(taskID, done, total, fileName, "分类")
		})
		if rmErr := os.RemoveAll(taskDir); rmErr != nil {
			log.Printf("清理上传临时目录失败 %s: %v", taskDir, rmErr)
		}
		taskMu.Lock()
		defer taskMu.Unlock()
		if err != nil {
			taskStatus[taskID].Status = "failed"
			taskStatus[taskID].Message = err.Error()
		} else {
			taskStatus[taskID].Status = "completed"
			taskStatus[taskID].Message = "分类完成"
			taskStatus[taskID].Progress = 100
			if taskStatus[taskID].Total > 0 {
				taskStatus[taskID].Current = taskStatus[taskID].Total
			}
		}
	}()

	json.NewEncoder(w).Encode(map[string]string{"message": "分类已启动"})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimPrefix(r.URL.Path, "/api/status/")
	taskMu.Lock()
	ts := taskStatus[taskID]
	taskMu.Unlock()
	if ts == nil {
		jsonError(w, "任务不存在", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(ts)
}

func handleDownload(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimPrefix(r.URL.Path, "/api/download/")
	taskMu.Lock()
	ts := taskStatus[taskID]
	taskMu.Unlock()
	if ts == nil || ts.Status != "completed" {
		jsonError(w, "任务未完成或不存在", http.StatusBadRequest)
		return
	}

	taskResultsDir := taskResultsPath(ts)
	zipName := "results.zip"
	if ts.Type == TaskTypeRepair {
		zipName = "repaired.zip"
	}

	zipPath := filepath.Join(filepath.Dir(taskResultsDir), taskID+".zip")
	if err := zipDir(taskResultsDir, zipPath); err != nil {
		jsonError(w, "打包失败", http.StatusInternalServerError)
		return
	}
	defer os.Remove(zipPath)

	w.Header().Set("Content-Disposition", "attachment; filename="+zipName)
	w.Header().Set("Content-Type", "application/zip")
	f, _ := os.Open(zipPath)
	defer f.Close()
	io.Copy(w, f)
}

func zipDir(src, dest string) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	defer zw.Close()

	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(src, path)
		zf, err := zw.Create(rel)
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, err = io.Copy(zf, file)
		file.Close()
		return err
	})
}

type ResultRow struct {
	BookName           string `json:"book_name"`
	Classification     string `json:"classification"`
	ClassificationPath string `json:"classification_path"`
	Author             string `json:"author"`
	Nationality        string `json:"nationality"`
	Error              string `json:"error,omitempty"`
}

func handleResults(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimPrefix(r.URL.Path, "/api/results/")
	taskMu.Lock()
	ts := taskStatus[taskID]
	taskMu.Unlock()
	if ts == nil || ts.Status != "completed" {
		jsonError(w, "任务未完成或不存在", http.StatusBadRequest)
		return
	}

	resultFile := filepath.Join(resultsDir, taskID, "结果.txt")
	data, err := os.ReadFile(resultFile)
	if err != nil {
		jsonError(w, "结果文件不存在", http.StatusNotFound)
		return
	}

	lines := strings.Split(string(data), "\n")
	var rows []ResultRow
	for i, line := range lines {
		if i < 2 || strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		for j := range parts {
			parts[j] = strings.TrimSpace(parts[j])
		}
		if len(parts) >= 5 {
			rows = append(rows, ResultRow{
				BookName:           parts[0],
				Classification:     parts[1],
				ClassificationPath: parts[2],
				Author:             parts[3],
				Nationality:        parts[4],
			})
		} else if len(parts) >= 4 {
			rows = append(rows, ResultRow{
				BookName:       parts[0],
				Classification: parts[1],
				Author:         parts[2],
				Nationality:    parts[3],
			})
		} else if len(parts) >= 2 {
			rows = append(rows, ResultRow{
				BookName: parts[0],
				Error:    parts[1],
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rows)
}

func handleRepairUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	taskID := fmt.Sprintf("%d", time.Now().UnixNano())
	taskDir := filepath.Join(repairUploadDir, taskID)
	os.MkdirAll(taskDir, 0755)

	if err := r.ParseMultipartForm(100 << 20); err != nil {
		jsonError(w, "解析表单失败", http.StatusBadRequest)
		return
	}

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		jsonError(w, "请选择要上传的文件", http.StatusBadRequest)
		return
	}

	uploaded := 0
	for _, fh := range files {
		if !service.IsTxtFile(fh.Filename) {
			continue
		}
		rel := filepath.Clean(filepath.FromSlash(fh.Filename))
		if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			continue
		}
		f, err := fh.Open()
		if err != nil {
			continue
		}
		dst := filepath.Join(taskDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			f.Close()
			continue
		}
		out, err := os.Create(dst)
		if err != nil {
			f.Close()
			continue
		}
		io.Copy(out, f)
		f.Close()
		out.Close()
		uploaded++
	}

	if uploaded == 0 {
		os.RemoveAll(taskDir)
		jsonError(w, "未找到符合条件的 txt 文件", http.StatusBadRequest)
		return
	}

	taskMu.Lock()
	taskStatus[taskID] = &TaskStatus{
		ID: taskID, Type: TaskTypeRepair, Status: "pending", Message: "上传完成",
		Total: uploaded, Current: 0, Progress: 0,
		CreatedAt: time.Now(),
	}
	taskMu.Unlock()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"task_id": taskID,
		"count":   uploaded,
	})
}

func handleRepair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		TaskID string `json:"task_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TaskID == "" {
		jsonError(w, "缺少 task_id", http.StatusBadRequest)
		return
	}

	taskDir := filepath.Join(repairUploadDir, req.TaskID)
	taskResultsDir := filepath.Join(repairResultsDir, req.TaskID)

	if _, err := os.Stat(taskDir); os.IsNotExist(err) {
		jsonError(w, "任务不存在", http.StatusNotFound)
		return
	}

	taskMu.Lock()
	if taskStatus[req.TaskID] == nil {
		taskMu.Unlock()
		jsonError(w, "任务不存在", http.StatusNotFound)
		return
	}
	taskStatus[req.TaskID].Status = "processing"
	taskStatus[req.TaskID].Message = "正在准备修复..."
	taskStatus[req.TaskID].Progress = 0
	taskMu.Unlock()

	taskID := req.TaskID
	go func() {
		os.MkdirAll(taskResultsDir, 0755)
		err := service.RepairBooks(taskDir, taskResultsDir, func(done, total int, fileName string) {
			updateTaskProgress(taskID, done, total, fileName, "修复")
		})
		if rmErr := os.RemoveAll(taskDir); rmErr != nil {
			log.Printf("清理修复上传临时目录失败 %s: %v", taskDir, rmErr)
		}
		taskMu.Lock()
		defer taskMu.Unlock()
		if err != nil {
			taskStatus[taskID].Status = "failed"
			taskStatus[taskID].Message = err.Error()
		} else {
			taskStatus[taskID].Status = "completed"
			taskStatus[taskID].Message = "修复完成"
			taskStatus[taskID].Progress = 100
			if taskStatus[taskID].Total > 0 {
				taskStatus[taskID].Current = taskStatus[taskID].Total
			}
		}
	}()

	json.NewEncoder(w).Encode(map[string]string{"message": "修复已启动"})
}

type RepairResultRow struct {
	OriginalName    string   `json:"original_name"`
	NewName         string   `json:"new_name"`
	Encoding        string   `json:"encoding"`
	OutputEncoding  string   `json:"output_encoding"`
	OriginalBytes   int      `json:"original_bytes"`
	FinalBytes      int      `json:"final_bytes"`
	OriginalRunes   int      `json:"original_runes"`
	FinalRunes      int      `json:"final_runes"`
	OriginalLines   int      `json:"original_lines"`
	FinalLines      int      `json:"final_lines"`
	RemovedLines    int      `json:"removed_lines"`
	RemovedForum    int      `json:"removed_forum_lines"`
	RemovedAd       int      `json:"removed_ad_lines"`
	CollapsedBlank  int      `json:"collapsed_blank_lines"`
	FilenameChanges []string `json:"filename_changes"`
	Optimizations   []string `json:"optimizations"`
	Error           string   `json:"error,omitempty"`
}

func handleRepairResults(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimPrefix(r.URL.Path, "/api/repair/results/")
	taskMu.Lock()
	ts := taskStatus[taskID]
	taskMu.Unlock()
	if ts == nil || ts.Status != "completed" || ts.Type != TaskTypeRepair {
		jsonError(w, "任务未完成或不存在", http.StatusBadRequest)
		return
	}

	resultsDir := filepath.Join(repairResultsDir, taskID)
	report, err := service.LoadRepairReport(resultsDir)
	if err == nil {
		rows := make([]RepairResultRow, 0, len(report.Items))
		for _, item := range report.Items {
			rows = append(rows, repairItemToRow(item))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rows)
		return
	}

	// 兼容旧版纯文本报告
	reportFile := filepath.Join(resultsDir, service.RepairReportFile)
	data, readErr := os.ReadFile(reportFile)
	if readErr != nil {
		jsonError(w, "修复报告不存在", http.StatusNotFound)
		return
	}

	lines := strings.Split(string(data), "\n")
	var rows []RepairResultRow
	for i, line := range lines {
		if i < 4 || strings.TrimSpace(line) == "" || strings.HasPrefix(line, "-") || strings.HasPrefix(line, "=") || strings.HasPrefix(line, "[") {
			continue
		}
		parts := strings.Split(line, "\t")
		for j := range parts {
			parts[j] = strings.TrimSpace(parts[j])
		}
		if len(parts) < 5 {
			continue
		}
		row := RepairResultRow{
			OriginalName: parts[0],
			NewName:      parts[1],
			Encoding:     parts[2],
		}
		fmt.Sscanf(parts[3], "%d", &row.RemovedLines)
		status := parts[4]
		if status != "成功" {
			row.Error = status
		}
		rows = append(rows, row)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rows)
}

func repairItemToRow(item service.RepairResultItem) RepairResultRow {
	row := RepairResultRow{
		OriginalName:    item.OriginalName,
		NewName:         item.NewName,
		Encoding:        item.Stats.Encoding,
		OutputEncoding:  item.Stats.OutputEncoding,
		OriginalBytes:   item.Stats.OriginalBytes,
		FinalBytes:      item.Stats.FinalBytes,
		OriginalRunes:   item.Stats.OriginalRunes,
		FinalRunes:      item.Stats.FinalRunes,
		OriginalLines:   item.Stats.OriginalLines,
		FinalLines:      item.Stats.FinalLines,
		RemovedLines:    item.Stats.RemovedLines + item.Stats.CollapsedBlankLines + item.Stats.ForumHeaderLines + item.Stats.ForumFooterLines,
		RemovedForum:    item.Stats.RemovedForumLines,
		RemovedAd:       item.Stats.RemovedAdLines,
		CollapsedBlank:  item.Stats.CollapsedBlankLines,
		FilenameChanges: item.FilenameChanges,
		Optimizations:   item.Stats.Optimizations,
		Error:           item.Error,
	}
	return row
}

func taskResultsPath(ts *TaskStatus) string {
	if ts.Type == TaskTypeRepair {
		return filepath.Join(repairResultsDir, ts.ID)
	}
	return filepath.Join(resultsDir, ts.ID)
}

var dataTaskDirs = []string{uploadDir, resultsDir, repairUploadDir, repairResultsDir}

func handleClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	taskMu.Lock()
	defer taskMu.Unlock()

	for _, dir := range dataTaskDirs {
		if err := clearTaskDataDir(dir); err != nil {
			jsonError(w, fmt.Sprintf("清理 %s 失败: %v", dir, err), http.StatusInternalServerError)
			return
		}
	}

	taskStatus = make(map[string]*TaskStatus)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "已清空本地任务数据"})
}

func clearTaskDataDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(dir, 0755)
		}
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
