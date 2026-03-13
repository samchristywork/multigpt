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

type question struct {
	question string
	answer   string
	tokens   int
	duration time.Duration
	err      string
}

type ollamaResponse struct {
	Response        string `json:"response"`
	EvalCount       int    `json:"eval_count"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	TotalDuration   int64  `json:"total_duration"`
	Done            bool   `json:"done"`
	Error           string `json:"error"`
}

func ask(ollamaURL string, model string, think bool, system string, query string, timeout time.Duration) (string, int, time.Duration, string) {
	client := &http.Client{Timeout: timeout}
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
		return "", 0, 0, err.Error()
	}

	resp, err := client.Post(ollamaURL+"/api/generate", "application/json", bytes.NewReader(data))
	if err != nil {
		return "", 0, 0, err.Error()
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, 0, err.Error()
	}

	var result ollamaResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", 0, 0, err.Error()
	}

	if result.Error != "" {
		return "", 0, 0, result.Error
	}

	text := strings.ReplaceAll(result.Response, "\n", " ")
	return text, result.EvalCount + result.PromptEvalCount, time.Duration(result.TotalDuration), ""
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
		if line := scanner.Text(); line != "" {
			lines = append(lines, line)
		}
	}
	return lines, scanner.Err()
}

func main() {
	role := flag.String("role", "You are a helpful assistant.", "System prompt for the AI.")
	inputFile := flag.String("input", "input.txt", "Input file.")
	model := flag.String("model", "gemma3:4b", "Ollama model to use.")
	think := flag.Bool("think", false, "Enable think mode.")
	ollamaURL := flag.String("url", "http://192.168.0.15:11434", "Ollama server URL.")
	timeoutSecs := flag.Int("timeout", 120, "HTTP timeout in seconds per query.")

	flag.Parse()

	for _, line := range []string{
		"URL: " + *ollamaURL,
		"Model: " + *model,
		"Role: " + *role,
		"Input file: " + *inputFile,
	} {
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
			questions[n].answer, questions[n].tokens, questions[n].duration, questions[n].err = ask(*ollamaURL, *model, *think, *role, questions[n].question, time.Duration(*timeoutSecs)*time.Second)
		}(n)
	}
	wg.Wait()

	totalTokens := 0
	for _, q := range questions {
		if q.err != "" {
			fmt.Fprintf(os.Stderr, "error: %s: %s\n", q.question, q.err)
			continue
		}
		fmt.Printf("%s\t%s\t[%d tokens, %.2fs]\n", q.question, q.answer, q.tokens, q.duration.Seconds())
		totalTokens += q.tokens
	}

	fmt.Println("Total tokens:", totalTokens)
}
