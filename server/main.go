package main

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	uploadDir        = "data/uploads"
	resultsDir       = "data/results"
	repairUploadDir  = "data/repair_uploads"
	repairResultsDir = "data/repair_results"
	port             = "8080"
	maxUploadBytes   = 100 << 20
	maxFileBytes     = 50 << 20
	maxUploadFiles   = 100
	maxJSONBodyBytes = 1 << 20
	taskStateFile    = "data/tasks.json"
	defaultTaskTTL   = 7 * 24 * time.Hour
	defaultWorkers   = 2
)

const (
	TaskTypeClassify = "classify"
	TaskTypeRepair   = "repair"
)

var (
	config         *service.Config
	client         *service.DeepSeekClient
	apiToken       string
	allowedOrigins map[string]struct{}
	taskRetention  time.Duration
	taskMu         sync.RWMutex
	taskStatus     = make(map[string]*TaskStatus)
	taskCancels    = make(map[string]context.CancelFunc)
	taskQueue      chan string
)

type TaskStatus struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`   // classify, repair
	Status    string    `json:"status"` // pending, queued, processing, completed, failed, cancelled
	Message   string    `json:"message"`
	Total     int       `json:"total,omitempty"`
	Current   int       `json:"current,omitempty"`
	Progress  int       `json:"progress,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func newTaskID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func validTaskID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func taskSnapshot(taskID string) *TaskStatus {
	taskMu.RLock()
	defer taskMu.RUnlock()
	ts := taskStatus[taskID]
	if ts == nil {
		return nil
	}
	copy := *ts
	return &copy
}

func enqueueTask(taskID, taskType, message string) error {
	taskMu.Lock()
	ts := taskStatus[taskID]
	if ts == nil || ts.Type != taskType {
		taskMu.Unlock()
		return fmt.Errorf("任务不存在")
	}
	if ts.Status != "pending" {
		taskMu.Unlock()
		return fmt.Errorf("任务当前状态为 %s，不能重复启动", ts.Status)
	}
	ts.Status = "queued"
	ts.Message = message
	ts.Progress = 0
	persistTaskStateLocked()
	taskMu.Unlock()
	select {
	case taskQueue <- taskID:
		return nil
	default:
		taskMu.Lock()
		if current := taskStatus[taskID]; current != nil && current.Status == "queued" {
			current.Status = "failed"
			current.Message = "任务队列已满，请稍后重试"
			persistTaskStateLocked()
		}
		taskMu.Unlock()
		return fmt.Errorf("任务队列已满")
	}

}

func startTaskWorkers(count int) {
	if count < 1 {
		count = defaultWorkers
	}
	taskQueue = make(chan string, count*16)
	for i := 0; i < count; i++ {
		go func() {
			for taskID := range taskQueue {
				runTask(taskID)
			}
		}()
	}
}

func runTask(taskID string) {
	taskMu.Lock()
	ts := taskStatus[taskID]
	if ts == nil || ts.Status != "queued" {
		taskMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	taskCancels[taskID] = cancel
	taskType := ts.Type
	ts.Status = "processing"
	ts.Message = "正在准备处理..."
	persistTaskStateLocked()
	taskMu.Unlock()

	var err error
	if taskType == TaskTypeClassify {
		taskDir := filepath.Join(uploadDir, taskID)
		resultDir := filepath.Join(resultsDir, taskID)
		err = service.ClassifyAndMoveContext(ctx, taskDir, resultDir, client, config.CLCIndexFile, func(done, total int, fileName string) {
			updateTaskProgress(taskID, done, total, fileName, "分类")
		})
		if rmErr := os.RemoveAll(taskDir); rmErr != nil {
			log.Printf("清理上传临时目录失败 %s: %v", taskDir, rmErr)
		}
	} else {
		taskDir := filepath.Join(repairUploadDir, taskID)
		resultDir := filepath.Join(repairResultsDir, taskID)
		err = service.RepairBooksContext(ctx, taskDir, resultDir, func(done, total int, fileName string) {
			updateTaskProgress(taskID, done, total, fileName, "修复")
		})
		if rmErr := os.RemoveAll(taskDir); rmErr != nil {
			log.Printf("清理修复上传临时目录失败 %s: %v", taskDir, rmErr)
		}
	}
	cancel()

	taskMu.Lock()
	defer taskMu.Unlock()
	delete(taskCancels, taskID)
	ts = taskStatus[taskID]
	if ts == nil {
		return
	}
	if errors.Is(err, context.Canceled) {
		ts.Status = "cancelled"
		ts.Message = "任务已取消"
	} else if err != nil {
		ts.Status = "failed"
		ts.Message = err.Error()
	} else {
		ts.Status = "completed"
		ts.Message = "处理完成"
		ts.Progress = 100
		if ts.Total > 0 {
			ts.Current = ts.Total
		}
	}
	persistTaskStateLocked()
}

func cancelTask(taskID string) error {
	taskMu.Lock()
	defer taskMu.Unlock()
	ts := taskStatus[taskID]
	if ts == nil {
		return fmt.Errorf("任务不存在")
	}
	switch ts.Status {
	case "queued", "pending":
		ts.Status = "cancelled"
		ts.Message = "任务已取消"
	case "processing":
		cancel := taskCancels[taskID]
		if cancel == nil {
			return fmt.Errorf("任务正在启动，请稍后重试")
		}
		ts.Message = "正在取消任务..."
		cancel()
	default:
		return fmt.Errorf("任务当前状态为 %s，不能取消", ts.Status)
	}
	persistTaskStateLocked()
	return nil
}

func persistTaskStateLocked() {
	state := make(map[string]TaskStatus, len(taskStatus))
	for id, status := range taskStatus {
		state[id] = *status
	}
	if err := os.MkdirAll(filepath.Dir(taskStateFile), 0755); err != nil {
		log.Printf("创建任务状态目录失败: %v", err)
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(taskStateFile), ".tasks-*.json")
	if err != nil {
		log.Printf("创建任务状态临时文件失败: %v", err)
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	encoder := json.NewEncoder(tmp)
	if err := encoder.Encode(state); err != nil {
		tmp.Close()
		log.Printf("写入任务状态失败: %v", err)
		return
	}
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		log.Printf("设置任务状态文件权限失败: %v", err)
		return
	}
	if err := tmp.Close(); err != nil {
		log.Printf("关闭任务状态文件失败: %v", err)
		return
	}
	if err := os.Rename(tmpPath, taskStateFile); err != nil {
		log.Printf("保存任务状态失败: %v", err)
	}
}

func loadTaskState() {
	data, err := os.ReadFile(taskStateFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("读取任务状态失败: %v", err)
		}
		return
	}
	var state map[string]TaskStatus
	if err := json.Unmarshal(data, &state); err != nil {
		log.Printf("解析任务状态失败: %v", err)
		return
	}

	taskMu.Lock()
	defer taskMu.Unlock()
	interrupted := 0
	for id, status := range state {
		if !validTaskID(id) || status.ID != id || (status.Type != TaskTypeClassify && status.Type != TaskTypeRepair) {
			continue
		}
		if status.Status == "processing" || status.Status == "queued" || status.Status == "pending" {
			status.Status = "failed"
			status.Message = "服务重启导致任务中断，请重新上传并处理"
			interrupted++
		}
		copy := status
		taskStatus[id] = &copy
	}
	if interrupted > 0 {
		persistTaskStateLocked()
		log.Printf("已将 %d 个中断任务标记为失败", interrupted)
	}
}

func cleanupExpiredTasks() {
	if taskRetention <= 0 {
		return
	}
	cutoff := time.Now().Add(-taskRetention)
	taskMu.Lock()
	defer taskMu.Unlock()
	changed := false
	for id, status := range taskStatus {
		if status.Status == "processing" || status.Status == "queued" || status.CreatedAt.IsZero() || status.CreatedAt.After(cutoff) {
			continue
		}
		if err := removeTaskData(id); err != nil {
			log.Printf("清理过期任务 %s 失败: %v", id, err)
			continue
		}
		delete(taskStatus, id)
		changed = true
	}
	if changed {
		persistTaskStateLocked()
	}
}

func removeTaskData(taskID string) error {
	if !validTaskID(taskID) {
		return fmt.Errorf("无效 task_id")
	}
	for _, dir := range []string{uploadDir, resultsDir, repairUploadDir, repairResultsDir} {
		if err := os.RemoveAll(filepath.Join(dir, taskID)); err != nil {
			return err
		}
	}
	return nil
}

func startTaskCleanupLoop() {
	if taskRetention <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			cleanupExpiredTasks()
		}
	}()
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
	apiToken = strings.TrimSpace(os.Getenv("BOOK_DISTRIBUTE_API_TOKEN"))
	if apiToken == "" {
		apiToken = strings.TrimSpace(config.APIAuthToken)
	}
	allowedOrigins = makeAllowedOrigins(config.AllowedOrigins)
	taskRetention = defaultTaskTTL
	if config.TaskRetentionHours > 0 {
		taskRetention = time.Duration(config.TaskRetentionHours) * time.Hour
	}
	workers := config.MaxConcurrentTasks
	if workers < 1 {
		workers = defaultWorkers
	}
	if apiToken == "" {
		log.Printf("警告: 未配置 API Token；服务仅适合受信任的本地网络")
	}

	for _, dir := range dataTaskDirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatalf("创建运行时目录 %s 失败: %v", dir, err)
		}
	}
	loadTaskState()
	cleanupExpiredTasks()
	startTaskCleanupLoop()
	startTaskWorkers(workers)

	mux := http.NewServeMux()
	registerRoutes(mux)

	log.Printf("后端服务启动: http://localhost:%s", port)
	server := &http.Server{
		Addr:              ":" + port,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       2 * time.Minute,
		WriteTimeout:      10 * time.Minute,
		IdleTimeout:       time.Minute,
		MaxHeaderBytes:    1 << 20,
		Handler:           mux,
	}
	log.Fatal(server.ListenAndServe())
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	taskID, err := newTaskID()
	if err != nil {
		jsonError(w, "创建任务失败", http.StatusInternalServerError)
		return
	}
	taskDir := filepath.Join(uploadDir, taskID)
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		jsonError(w, "创建任务目录失败", http.StatusInternalServerError)
		return
	}
	defer func() {
		if taskSnapshot(taskID) == nil {
			_ = os.RemoveAll(taskDir)
		}
	}()

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		jsonError(w, "解析表单失败", http.StatusBadRequest)
		return
	}

	files := r.MultipartForm.File["files"]
	if len(files) == 0 || len(files) > maxUploadFiles {
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
		if fh.Size > maxFileBytes {
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
		written, copyErr := io.Copy(out, io.LimitReader(f, maxFileBytes+1))
		closeErr := out.Close()
		f.Close()
		if copyErr != nil || closeErr != nil || written > maxFileBytes {
			_ = os.Remove(dst)
			continue
		}
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
	persistTaskStateLocked()
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
	if err := decodeJSON(w, r, &req); err != nil || req.TaskID == "" {
		jsonError(w, "缺少 task_id", http.StatusBadRequest)
		return
	}

	if !validTaskID(req.TaskID) {
		jsonError(w, "无效 task_id", http.StatusBadRequest)
		return
	}
	taskDir := filepath.Join(uploadDir, req.TaskID)

	if _, err := os.Stat(taskDir); os.IsNotExist(err) {
		jsonError(w, "任务不存在", http.StatusNotFound)
		return
	}

	if err := enqueueTask(req.TaskID, TaskTypeClassify, "任务已进入分类队列..."); err != nil {
		jsonError(w, err.Error(), http.StatusConflict)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"message": "分类任务已入队"})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimPrefix(r.URL.Path, "/api/status/")
	if !validTaskID(taskID) {
		jsonError(w, "无效 task_id", http.StatusBadRequest)
		return
	}
	ts := taskSnapshot(taskID)
	if ts == nil {
		jsonError(w, "任务不存在", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(ts)
}

func handleDownload(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimPrefix(r.URL.Path, "/api/download/")
	if !validTaskID(taskID) {
		jsonError(w, "无效 task_id", http.StatusBadRequest)
		return
	}
	ts := taskSnapshot(taskID)
	if ts == nil || ts.Status != "completed" {
		jsonError(w, "任务未完成或不存在", http.StatusBadRequest)
		return
	}

	taskResultsDir := taskResultsPath(ts)
	zipName := "results.zip"
	if ts.Type == TaskTypeRepair {
		zipName = "repaired.zip"
	}

	tmp, err := os.CreateTemp(filepath.Dir(taskResultsDir), ".book-distribute-download-*.zip")
	if err != nil {
		jsonError(w, "创建下载文件失败", http.StatusInternalServerError)
		return
	}
	zipPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(zipPath)
		jsonError(w, "创建下载文件失败", http.StatusInternalServerError)
		return
	}
	if err := zipDir(taskResultsDir, zipPath); err != nil {
		_ = os.Remove(zipPath)
		jsonError(w, "打包失败", http.StatusInternalServerError)
		return
	}
	defer os.Remove(zipPath)

	w.Header().Set("Content-Disposition", "attachment; filename="+zipName)
	w.Header().Set("Content-Type", "application/zip")
	f, err := os.Open(zipPath)
	if err != nil {
		jsonError(w, "打开下载文件失败", http.StatusInternalServerError)
		return
	}
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
	if !validTaskID(taskID) {
		jsonError(w, "无效 task_id", http.StatusBadRequest)
		return
	}
	ts := taskSnapshot(taskID)
	if ts == nil || ts.Status != "completed" || ts.Type != TaskTypeClassify {
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

	taskID, err := newTaskID()
	if err != nil {
		jsonError(w, "创建任务失败", http.StatusInternalServerError)
		return
	}
	taskDir := filepath.Join(repairUploadDir, taskID)
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		jsonError(w, "创建任务目录失败", http.StatusInternalServerError)
		return
	}
	defer func() {
		if taskSnapshot(taskID) == nil {
			_ = os.RemoveAll(taskDir)
		}
	}()

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		jsonError(w, "解析表单失败", http.StatusBadRequest)
		return
	}

	files := r.MultipartForm.File["files"]
	if len(files) == 0 || len(files) > maxUploadFiles {
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
		if fh.Size > maxFileBytes {
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
		written, copyErr := io.Copy(out, io.LimitReader(f, maxFileBytes+1))
		closeErr := out.Close()
		f.Close()
		if copyErr != nil || closeErr != nil || written > maxFileBytes {
			_ = os.Remove(dst)
			continue
		}
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
	persistTaskStateLocked()
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
	if err := decodeJSON(w, r, &req); err != nil || req.TaskID == "" {
		jsonError(w, "缺少 task_id", http.StatusBadRequest)
		return
	}

	if !validTaskID(req.TaskID) {
		jsonError(w, "无效 task_id", http.StatusBadRequest)
		return
	}
	taskDir := filepath.Join(repairUploadDir, req.TaskID)

	if _, err := os.Stat(taskDir); os.IsNotExist(err) {
		jsonError(w, "任务不存在", http.StatusNotFound)
		return
	}

	if err := enqueueTask(req.TaskID, TaskTypeRepair, "任务已进入修复队列..."); err != nil {
		jsonError(w, err.Error(), http.StatusConflict)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"message": "修复任务已入队"})
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
	if !validTaskID(taskID) {
		jsonError(w, "无效 task_id", http.StatusBadRequest)
		return
	}
	ts := taskSnapshot(taskID)
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
	for _, ts := range taskStatus {
		if ts.Status == "processing" || ts.Status == "queued" {
			jsonError(w, "存在正在处理的任务，不能清空", http.StatusConflict)
			return
		}
	}

	for _, dir := range dataTaskDirs {
		if err := clearTaskDataDir(dir); err != nil {
			jsonError(w, fmt.Sprintf("清理 %s 失败: %v", dir, err), http.StatusInternalServerError)
			return
		}
	}

	taskStatus = make(map[string]*TaskStatus)
	persistTaskStateLocked()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "已清空本地任务数据"})
}

func handleCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		TaskID string `json:"task_id"`
	}
	if err := decodeJSON(w, r, &req); err != nil || !validTaskID(req.TaskID) {
		jsonError(w, "无效 task_id", http.StatusBadRequest)
		return
	}
	if err := cancelTask(req.TaskID); err != nil {
		jsonError(w, err.Error(), http.StatusConflict)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"message": "取消请求已提交"})
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
