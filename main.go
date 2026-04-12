package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// The JSON contract from React
type ExecuteRequest struct {
	TaskID string   `json:"taskId"`
	Code   string   `json:"code"`
	Args   []string `json:"args"`
}

// The JSON contract going back to React
type ExecuteResponse struct {
	Output []string `json:"output"`
	Error  *string  `json:"error"`
}

func main() {
	mux := http.NewServeMux()

	// 1. The Wake-Up Endpoint
	mux.HandleFunc("/api/ping", handleCORS(pingHandler))
	
	// 2. The Execution Engine Endpoint
	mux.HandleFunc("/api/execute", handleCORS(executeCodeHandler))

	// Get port from Render, fallback to 8080 for local dev
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("GOPHER_OS Execution Engine running on port %s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

// pingHandler responds instantly to wake up the Render instance
func pingHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodOptions {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"awake", "message":"GOPHER_OS Engine is online"}`))
}

// executeCodeHandler compiles and runs the submitted Go code
func executeCodeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodOptions {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, "Invalid JSON payload")
		return
	}

	tmpDir, err := os.MkdirTemp("", "gopher_exec_*")
	if err != nil {
		sendError(w, "Failed to create execution environment")
		return
	}
	// CLEANUP: Ensure the folder is deleted when the function finishes
	defer os.RemoveAll(tmpDir)

	filePath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(filePath, []byte(req.Code), 0644); err != nil {
		sendError(w, "Failed to write code to disk")
		return
	}

	// Set a strict timeout to prevent infinite loops (5 seconds)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmdArgs := append([]string{"run", "main.go"}, req.Args...)
	cmd := exec.CommandContext(ctx, "go", cmdArgs...)
	cmd.Dir = tmpDir 

	var outBuf bytes.Buffer
	var errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err = cmd.Run()

	response := ExecuteResponse{
		Output: []string{},
		Error:  nil,
	}

	if err != nil {
		errMsg := errBuf.String()
		
		if ctx.Err() == context.DeadlineExceeded {
			errMsg = "Execution Error: Time limit exceeded (5 seconds). Do you have an infinite loop?"
		} else if errMsg == "" {
			errMsg = err.Error() 
		}

		// Clean up messy absolute paths from Go compiler errors
		cleanErr := strings.ReplaceAll(errMsg, tmpDir+"/", "")
		response.Error = &cleanErr
	} else {
		rawOutput := strings.TrimSpace(outBuf.String())
		if rawOutput != "" {
			response.Output = strings.Split(rawOutput, "\n")
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Helper to format HTTP errors into our JSON structure
func sendError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(ExecuteResponse{
		Output: []string{},
		Error:  &msg,
	})
}

// CORS Middleware
func handleCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*") 
		// Added GET to allowed methods for the ping endpoint
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}