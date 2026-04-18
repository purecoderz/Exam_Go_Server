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

// ⚡️ NEW: TestCase Struct for grading
type TestCase struct {
	Args           []string `json:"args"`
	ExpectedOutput string   `json:"expectedOutput"`
}

type ExecuteRequest struct {
	TaskID          string     `json:"taskId"`
	Mode            string     `json:"mode"`
	StudentMainCode string     `json:"studentMainCode"`
	StudentSolution string     `json:"studentSolution"`
	HiddenMainCode  string     `json:"hiddenMainCode"`
	SolutionName    string     `json:"solutionName"`
	Args            []string   `json:"args"`
	Tests           []TestCase `json:"tests"` // ⚡️ NEW: Array of tests from frontend
}

type ExecuteResponse struct {
	Output []string `json:"output"`
	Error  *string  `json:"error"`
	Passed bool     `json:"passed"`
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/ping", handleCORS(pingHandler))
	mux.HandleFunc("/api/execute", handleCORS(executeCodeHandler))

	port := os.Getenv("PORT")
	if port == "" {
		port = "3001"
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

	tmpDir, err := os.MkdirTemp("", "gopher_exec_*")
	if err != nil {
		sendError(w, "Failed to create execution environment")
		return
	}
	defer os.RemoveAll(tmpDir)

	// ==========================================
	// 1. FILE PREPARATION & MODULE INJECTION
	// ==========================================
	
	modData, err := os.ReadFile("/student_env/go.mod")
	if err == nil {
		os.WriteFile(filepath.Join(tmpDir, "go.mod"), modData, 0644)
	}
	
	sumData, err := os.ReadFile("/student_env/go.sum")
	if err == nil {
		os.WriteFile(filepath.Join(tmpDir, "go.sum"), sumData, 0644)
	}

	var filesToBuild []string
	var programArgs []string // Used for standard "run" or "single" modes

	switch req.Mode {
	case "single":
		mainPath := filepath.Join(tmpDir, "main.go")
		os.WriteFile(mainPath, []byte(req.StudentMainCode), 0644)
		filesToBuild = []string{"main.go"}
		programArgs = req.Args

	case "single_submit":
		mainPath := filepath.Join(tmpDir, "main.go")
		os.WriteFile(mainPath, []byte(req.StudentMainCode), 0644)
		filesToBuild = []string{"main.go"}
		// We don't set programArgs here because we will loop through req.Tests later

	case "run":
		mainPath := filepath.Join(tmpDir, "main.go")
		os.WriteFile(mainPath, []byte(req.StudentMainCode), 0644)
		solPath := filepath.Join(tmpDir, req.SolutionName)
		os.WriteFile(solPath, []byte(req.StudentSolution), 0644)
		filesToBuild = []string{"main.go", req.SolutionName}
		programArgs = req.Args

	case "submit":
		mainPath := filepath.Join(tmpDir, "main.go")
		os.WriteFile(mainPath, []byte(req.HiddenMainCode), 0644)
		solPath := filepath.Join(tmpDir, req.SolutionName)
		os.WriteFile(solPath, []byte(req.StudentSolution), 0644)
		filesToBuild = []string{"main.go", req.SolutionName}
		programArgs = []string{}

	default:
		sendError(w, "Invalid execution mode")
		return
	}

	// ==========================================
	// 2. THE TWO-STEP EXECUTION PROCESS
	// ==========================================
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()

	response := ExecuteResponse{
		Output: []string{},
		Passed: false,
	}

	// --- STEP A: COMPILE ---
	buildArgs := append([]string{"build", "-o", "student_app"}, filesToBuild...)
	buildCmd := exec.CommandContext(ctx, "go", buildArgs...)
	buildCmd.Dir = tmpDir

	var buildErrBuf bytes.Buffer
	buildCmd.Stderr = &buildErrBuf

	if err := buildCmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			msg := "Timeout: Compilation took too long."
			response.Error = &msg
		} else {
			cleanErr := strings.ReplaceAll(strings.TrimSpace(buildErrBuf.String()), tmpDir+"/", "")
			response.Error = &cleanErr
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// --- STEP B: RUN ---
	
	// ⚡️ BRANCH 1: Multi-Test Grading (Single Submit)
	if req.Mode == "single_submit" {
		allPassed := true

		for i, test := range req.Tests {
			runCmd := exec.CommandContext(ctx, "./student_app", test.Args...)
			runCmd.Dir = tmpDir

			var outBuf, errBuf bytes.Buffer
			runCmd.Stdout = &outBuf
			runCmd.Stderr = &errBuf

			runErr := runCmd.Run()
			
			// 1. Get the RAW exact strings
			rawActual := outBuf.String()
			rawExpected := test.ExpectedOutput

			// 2. Normalize line endings (CRLF to LF) to prevent Windows vs Linux false positives
			cleanActual := strings.ReplaceAll(rawActual, "\r\n", "\n")
			cleanExpected := strings.ReplaceAll(rawExpected, "\r\n", "\n")

			// Check for Crashes or Timeouts
			if runErr != nil {
				allPassed = false
				if ctx.Err() == context.DeadlineExceeded {
					timeoutMsg := fmt.Sprintf("Timeout on Test %d: Infinite loop?", i+1)
					response.Error = &timeoutMsg
				} else {
					rawErr := strings.TrimSpace(errBuf.String())
					if rawErr == "" {
						rawErr = "Exit status 1"
					}
					crashMsg := fmt.Sprintf("Crash on Test %d: %s", i+1, rawErr)
					response.Error = &crashMsg
				}
				break // Stop testing immediately
			}

			// ⚡️ RUTHLESS STRICT COMPARE
			if cleanActual != cleanExpected {
				allPassed = false
				
				// %q will wrap the output in double quotes and visually expose hidden newlines as \n
				failMsg := fmt.Sprintf("Test %d Failed.\nArgs: %v\nExpected: %q\nGot:      %q", i+1, test.Args, cleanExpected, cleanActual)
				response.Error = &failMsg
				break // Stop testing immediately
			}
		}

		response.Passed = allPassed

	// ⚡️ BRANCH 2: Standard Execution (Run / Single / Hidden Submit Graders)
	} else {
		runCmd := exec.CommandContext(ctx, "./student_app", programArgs...)
		runCmd.Dir = tmpDir

		var outBuf, errBuf bytes.Buffer
		runCmd.Stdout = &outBuf
		runCmd.Stderr = &errBuf

		runErr := runCmd.Run()

		rawOutput := strings.TrimSpace(outBuf.String())
		if rawOutput != "" {
			response.Output = strings.Split(rawOutput, "\n")
		}

		if runErr != nil {
			if ctx.Err() == context.DeadlineExceeded {
				timeoutMsg := "Timeout: Your code took too long to run (Infinite loop?)"
				response.Error = &timeoutMsg
			} else {
				rawErr := strings.TrimSpace(errBuf.String())
				if rawErr != "" {
					response.Error = &rawErr
				} else {
					// Silent fail (Hidden grader called os.Exit(1) without printing to stderr)
					response.Passed = false
				}
			}
		} else {
			if req.Mode == "submit" {
				response.Passed = true
			}
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
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next(w, r)
	}
}