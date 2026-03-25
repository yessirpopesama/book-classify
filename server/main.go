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
	uploadDir   = "data/uploads"
	resultsDir  = "data/results"
	port        = "8080"
	frontendPort = "5173"
)

var (
	config     *service.Config
	client     *service.DeepSeekClient
	taskMu     sync.Mutex
	taskStatus = make(map[string]*TaskStatus)
)

type TaskStatus struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"` // pending, processing, completed, failed
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

func main() {
	var err error
	config, err = service.LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	client = service.NewDeepSeekClient(config.DeepSeekAPIKey)

	os.MkdirAll(uploadDir, 0755)
	os.MkdirAll(resultsDir, 0755)

	http.HandleFunc("/api/upload", cors(handleUpload))
	http.HandleFunc("/api/classify", cors(handleClassify))
	http.HandleFunc("/api/status/", cors(handleStatus))
	http.HandleFunc("/api/download/", cors(handleDownload))
	http.HandleFunc("/api/results/", cors(handleResults))

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

	for _, fh := range files {
		ext := strings.ToLower(filepath.Ext(fh.Filename))
		if ext != ".txt" && ext != ".pdf" && ext != ".epub" {
			continue
		}
		f, err := fh.Open()
		if err != nil {
			continue
		}
		dst := filepath.Join(taskDir, fh.Filename)
		out, err := os.Create(dst)
		if err != nil {
			f.Close()
			continue
		}
		io.Copy(out, f)
		f.Close()
		out.Close()
	}

	taskMu.Lock()
	taskStatus[taskID] = &TaskStatus{ID: taskID, Status: "pending", Message: "上传完成", CreatedAt: time.Now()}
	taskMu.Unlock()

	json.NewEncoder(w).Encode(map[string]string{"task_id": taskID})
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
	taskStatus[req.TaskID].Message = "正在分类..."
	taskMu.Unlock()

	go func() {
		os.MkdirAll(taskResultsDir, 0755)
		err := service.ClassifyAndMove(taskDir, taskResultsDir, client)
		taskMu.Lock()
		defer taskMu.Unlock()
		if err != nil {
			taskStatus[req.TaskID].Status = "failed"
			taskStatus[req.TaskID].Message = err.Error()
		} else {
			taskStatus[req.TaskID].Status = "completed"
			taskStatus[req.TaskID].Message = "分类完成"
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
	taskResultsDir := filepath.Join(resultsDir, taskID)

	taskMu.Lock()
	ts := taskStatus[taskID]
	taskMu.Unlock()
	if ts == nil || ts.Status != "completed" {
		jsonError(w, "任务未完成或不存在", http.StatusBadRequest)
		return
	}

	zipPath := filepath.Join(resultsDir, taskID+".zip")
	if err := zipDir(taskResultsDir, zipPath); err != nil {
		jsonError(w, "打包失败", http.StatusInternalServerError)
		return
	}
	defer os.Remove(zipPath)

	w.Header().Set("Content-Disposition", "attachment; filename=results.zip")
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
	BookName       string `json:"book_name"`
	Classification string `json:"classification"`
	Author         string `json:"author"`
	Nationality    string `json:"nationality"`
	Error          string `json:"error,omitempty"`
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
		if len(parts) >= 4 {
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

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
