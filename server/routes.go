package main

import "net/http"

// registerRoutes 统一注册所有 HTTP API，避免启动逻辑与接口细节耦合。
func registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/upload", apiMiddleware(handleUpload))
	mux.HandleFunc("/api/classify", apiMiddleware(handleClassify))
	mux.HandleFunc("/api/repair/upload", apiMiddleware(handleRepairUpload))
	mux.HandleFunc("/api/repair/results/", apiMiddleware(handleRepairResults))
	mux.HandleFunc("/api/repair", apiMiddleware(handleRepair))
	mux.HandleFunc("/api/results/", apiMiddleware(handleResults))
	mux.HandleFunc("/api/status/", apiMiddleware(handleStatus))
	mux.HandleFunc("/api/download/", apiMiddleware(handleDownload))
	mux.HandleFunc("/api/clear", apiMiddleware(handleClear))
	mux.HandleFunc("/api/cancel", apiMiddleware(handleCancel))
}
