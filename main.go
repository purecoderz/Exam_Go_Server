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

// 1. Updated Request Struct to match React Payload
type ExecuteRequest struct {
	TaskID          string   `json:"taskId"`
	Mode            string   `json:"mode"`            // "single", "run", or "submit"
	StudentMainCode string   `json:"studentMainCode"` // Used in single and run
	StudentSolution string   `json:"studentSolution"` // Used in run and submit
	HiddenMainCode  string   `json:"hiddenMainCode"`  // Used in submit
	SolutionName    string   `json:"solutionName"`    // e.g., "solution.go"
	Args            []string `json:"args"`
}

// 2. Updated Response Struct
type ExecuteResponse struct {
	Output []string `json:"output"`
	Error  *string  `json:"error"`
	Passed bool     `json:"passed"` // Tells React if the submission was successful
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/ping", handleCORS(pingHandler))
	mux.HandleFunc("/api/execute", handleCORS(executeCodeHandler))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("GOPHER_OS Engine Live on port %s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

func executeCodeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		return
	}

	var req ExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, "Invalid JSON payload")
		return
	}

	// Create isolation
	tmpDir, err := os.MkdirTemp("", "gopher_exec_*")
	if err != nil {
		sendError(w, "Failed to create execution environment")
		return
	}
	defer os.RemoveAll(tmpDir)

	var runArgs []string

	// ==========================================
	// FILE SYSTEM SETUP BASED ON MODE
	// ==========================================
	switch req.Mode {
	case "single":
		// Only main.go exists
		mainPath := filepath.Join(tmpDir, "main.go")
		os.WriteFile(mainPath, []byte(req.StudentMainCode), 0644)
		runArgs = append([]string{"run", "main.go"}, req.Args...)

	case "run":
		// Playground: Student Main + Student Solution
		mainPath := filepath.Join(tmpDir, "main.go")
		os.WriteFile(mainPath, []byte(req.StudentMainCode), 0644)

		solPath := filepath.Join(tmpDir, req.SolutionName)
		os.WriteFile(solPath, []byte(req.StudentSolution), 0644)
		
		runArgs = append([]string{"run", "main.go", req.SolutionName}, req.Args...)

	case "submit":
		// Grading: Hidden Grader Main + Student Solution
		mainPath := filepath.Join(tmpDir, "main.go")
		os.WriteFile(mainPath, []byte(req.HiddenMainCode), 0644)

		solPath := filepath.Join(tmpDir, req.SolutionName)
		os.WriteFile(solPath, []byte(req.StudentSolution), 0644)
		
		// Typically, graders don't take CLI args, they use internal tests
		runArgs = []string{"run", "main.go", req.SolutionName}

	default:
		sendError(w, "Invalid execution mode")
		return
	}

	// ==========================================
	// EXECUTION
	// ==========================================
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", runArgs...)
	cmd.Dir = tmpDir

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	execErr := cmd.Run()

	// Prepare response
	response := ExecuteResponse{
		Output: []string{},
		Passed: false,
	}

	if execErr != nil {
		rawErr := errBuf.String()
		if ctx.Err() == context.DeadlineExceeded {
			rawErr = "Timeout: Your code took too long to run (Infinite loop?)"
		} else if rawErr == "" {
			rawErr = execErr.Error()
		}
		
		// Clean up internal paths so student doesn't see /tmp/gopher_exec_...
		cleanErr := strings.ReplaceAll(rawErr, tmpDir+"/", "")
		response.Error = &cleanErr
	} else {
		// SUCCESS
		rawOutput := strings.TrimSpace(outBuf.String())
		if rawOutput != "" {
			response.Output = strings.Split(rawOutput, "\n")
		}
		
		// If mode was submit and we reached here without error, they passed!
		if req.Mode == "submit" {
			response.Passed = true
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// --- HELPERS ---

func pingHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"awake"}`))
}

func sendError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ExecuteResponse{
		Error: &msg,
	})
}

func handleCORS(next http.HandlerFunc) http.HandlerFunc {
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