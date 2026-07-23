package main

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// makeAllowedOrigins 构建 API 的跨域白名单。未配置时仅允许本地前端。
func makeAllowedOrigins(configured []string) map[string]struct{} {
	origins := []string{"http://localhost:8887", "http://127.0.0.1:8887"}
	if len(configured) > 0 {
		origins = configured
	}
	set := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		if origin = strings.TrimSpace(origin); origin != "" {
			set[origin] = struct{}{}
		}
	}
	return set
}

// apiMiddleware 统一处理 CORS 预检和可选 Token 认证。
func apiMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if _, ok := allowedOrigins[origin]; !ok {
				jsonError(w, "不允许的请求来源", http.StatusForbidden)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Token")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if apiToken != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-API-Token")), []byte(apiToken)) != 1 {
			w.Header().Set("WWW-Authenticate", "X-API-Token")
			jsonError(w, "未授权", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// decodeJSON 限制大小、拒绝未知字段和多余 JSON 值。
func decodeJSON(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("请求体只能包含一个 JSON 对象")
	}
	return nil
}
