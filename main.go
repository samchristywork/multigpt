package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const ollamaURL = "http://192.168.0.15:11434"

type question struct {
	question string
	answer   string
	tokens   int
	duration time.Duration
}

type ollamaResponse struct {
	Response        string `json:"response"`
	EvalCount       int    `json:"eval_count"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	TotalDuration   int64  `json:"total_duration"`
	Done            bool   `json:"done"`
	Error           string `json:"error"`
}

func getDateTime() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

func ask(model string, think bool, system string, query string) (string, int, time.Duration) {
	type payload struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
		System string `json:"system"`
		Stream bool   `json:"stream"`
		Think  bool   `json:"think"`
	}

	data, err := json.Marshal(payload{
		Model:  model,
		Prompt: query,
		System: system,
		Stream: false,
		Think:  think,
	})
	if err != nil {
		fmt.Println(err)
		return "error", 0, 0
	}

	start := time.Now()
	resp, err := http.Post(ollamaURL+"/api/generate", "application/json", bytes.NewReader(data))
	if err != nil {
		fmt.Println(err)
		return "error", 0, 0
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	elapsed := time.Since(start)
	if err != nil {
		fmt.Println(err)
		return "error", 0, elapsed
	}

	var result ollamaResponse
	if err := json.Unmarshal(body, &result); err != nil {
		fmt.Println(err)
		return "error", 0, elapsed
	}

	if result.Error != "" {
		fmt.Println("Ollama error:", result.Error)
		return "error", 0, elapsed
	}

	text := strings.ReplaceAll(result.Response, "\n", " ")
	return text, result.EvalCount + result.PromptEvalCount, elapsed
}

func readLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func main() {
	role := flag.String("role", "You are a helpful assistant.", "System prompt for the AI.")
	inputFile := flag.String("input", "input.txt", "Input file.")
	model := flag.String("model", "gemma3:4b", "Ollama model to use.")
	think := flag.Bool("think", false, "Enable think mode.")

	flag.Parse()

	datetime := getDateTime()
	logFilename := "log_" + datetime + ".txt"
	logFile, err := os.Create(logFilename)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer logFile.Close()

	for _, line := range []string{
		"Model: " + *model,
		"Role: " + *role,
		"Input file: " + *inputFile,
		"Output file: " + logFilename,
	} {
		fmt.Fprintln(logFile, line)
		fmt.Println(line)
	}

	lines, err := readLines(*inputFile)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	questions := make([]question, len(lines))
	for i, line := range lines {
		questions[i].question = strings.ReplaceAll(line, "\t", " ")
	}

	var wg sync.WaitGroup
	for n := range questions {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			questions[n].answer, questions[n].tokens, questions[n].duration = ask(*model, *think, *role, questions[n].question)
		}(n)
	}
	wg.Wait()

	totalTokens := 0
	for _, q := range questions {
		line := fmt.Sprintf("%s\t%s\t[%d tokens, %.2fs]", q.question, q.answer, q.tokens, q.duration.Seconds())
		fmt.Fprintln(logFile, line)
		fmt.Println(line)
		totalTokens += q.tokens
	}

	summary := fmt.Sprintf("Total tokens: %d", totalTokens)
	fmt.Fprintln(logFile, summary)
	fmt.Println(summary)
}
