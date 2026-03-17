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

type outputFormat string

const (
	formatTSV   outputFormat = "tsv"
	formatPlain outputFormat = "plain"
	formatJSON  outputFormat = "json"
)

type question struct {
	question     string
	model        string
	answer       string
	tokens       int
	duration     time.Duration
	tokensPerSec float64
	err          string
}

type ollamaResponse struct {
	Response        string `json:"response"`
	EvalCount       int    `json:"eval_count"`
	EvalDuration    int64  `json:"eval_duration"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	TotalDuration   int64  `json:"total_duration"`
	Done            bool   `json:"done"`
	Error           string `json:"error"`
}

func ask(ollamaURL string, model string, think bool, system string, query string, timeout time.Duration, retries int) (string, int, time.Duration, float64, string) {
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
		return "", 0, 0, 0, err.Error()
	}

	var lastErr string
	for attempt := 0; attempt <= retries; attempt++ {
		resp, err := client.Post(ollamaURL+"/api/generate", "application/json", bytes.NewReader(data))
		if err != nil {
			lastErr = err.Error()
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err.Error()
			continue
		}

		var result ollamaResponse
		if err := json.Unmarshal(body, &result); err != nil {
			lastErr = err.Error()
			continue
		}

		if result.Error != "" {
			return "", 0, 0, 0, result.Error
		}

		var tokensPerSec float64
		if result.EvalDuration > 0 {
			tokensPerSec = float64(result.EvalCount) / (float64(result.EvalDuration) / 1e9)
		}

		return result.Response, result.EvalCount + result.PromptEvalCount, time.Duration(result.TotalDuration), tokensPerSec, ""
	}

	return "", 0, 0, 0, lastErr
}

func listModels(ollamaURL string, timeout time.Duration) {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(ollamaURL + "/api/tags")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}

	for _, m := range result.Models {
		fmt.Println(m.Name)
	}
}

func readLines(path string) ([]string, error) {
	var r io.Reader
	if path == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	}

	var lines []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		if line := scanner.Text(); line != "" {
			lines = append(lines, line)
		}
	}
	return lines, scanner.Err()
}

func main() {
	role := flag.String("role", "You are a helpful assistant.", "System prompt for the AI.")
	inputFile := flag.String("input", "-", "Input file (- for stdin).")
	model := flag.String("model", "gemma3:4b", "Ollama model to use.")
	think := flag.Bool("think", false, "Enable think mode.")
	ollamaURL := flag.String("url", "http://192.168.0.15:11434", "Ollama server URL.")
	timeoutSecs := flag.Int("timeout", 120, "HTTP timeout in seconds per query.")
	concurrency := flag.Int("j", 0, "Max concurrent requests (0 = unlimited).")
	listModelsFlag := flag.Bool("list-models", false, "List available models and exit.")
	format := flag.String("format", "tsv", "Output format: tsv, plain, or json.")
	retries := flag.Int("retries", 0, "Number of retries on transient errors.")

	flag.Parse()

	if *listModelsFlag {
		listModels(*ollamaURL, time.Duration(*timeoutSecs)*time.Second)
		return
	}

	models := strings.Split(*model, ",")
	for i, m := range models {
		models[i] = strings.TrimSpace(m)
	}

	fmt.Fprintln(os.Stderr, "URL: "+*ollamaURL)
	fmt.Fprintln(os.Stderr, "Models: "+strings.Join(models, ", "))
	fmt.Fprintln(os.Stderr, "Role: "+*role)
	if *inputFile != "-" {
		fmt.Fprintln(os.Stderr, "Input file: "+*inputFile)
	}

	lines, err := readLines(*inputFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}

	var questions []question
	for _, m := range models {
		for _, line := range lines {
			questions = append(questions, question{
				question: strings.ReplaceAll(line, "\t", " "),
				model:    m,
			})
		}
	}

	limit := *concurrency
	if limit <= 0 {
		limit = len(lines)
	}
	if limit < 1 {
		limit = 1
	}
	sem := make(chan struct{}, limit)

	n := len(lines)
	for i := range models {
		var wg sync.WaitGroup
		for j := 0; j < n; j++ {
			idx := i*n + j
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				questions[idx].answer, questions[idx].tokens, questions[idx].duration, questions[idx].tokensPerSec, questions[idx].err = ask(*ollamaURL, questions[idx].model, *think, *role, questions[idx].question, time.Duration(*timeoutSecs)*time.Second, *retries)
			}(idx)
		}
		wg.Wait()
	}

	totalTokens := 0
	var successful []question
	hadErrors := false
	for _, q := range questions {
		if q.err != "" {
			fmt.Fprintf(os.Stderr, "error: %s: %s\n", q.question, q.err)
			hadErrors = true
			continue
		}
		totalTokens += q.tokens
		successful = append(successful, q)
	}

	switch outputFormat(*format) {
	case formatJSON:
		type jsonQuestion struct {
			Question     string  `json:"question"`
			Model        string  `json:"model"`
			Answer       string  `json:"answer"`
			Tokens       int     `json:"tokens"`
			DurationSecs float64 `json:"duration_secs"`
			TokensPerSec float64 `json:"tokens_per_sec"`
		}
		results := make([]jsonQuestion, len(successful))
		for i, q := range successful {
			results[i] = jsonQuestion{q.question, q.model, q.answer, q.tokens, q.duration.Seconds(), q.tokensPerSec}
		}
		out := struct {
			Results     []jsonQuestion `json:"results"`
			TotalTokens int            `json:"total_tokens"`
		}{results, totalTokens}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(out)
	case formatPlain:
		for _, q := range successful {
			fmt.Printf("Q: %s\nM: %s\nA: %s\n   [%d tokens, %.2fs, %.1f tok/s]\n\n", q.question, q.model, q.answer, q.tokens, q.duration.Seconds(), q.tokensPerSec)
		}
		fmt.Println("Total tokens:", totalTokens)
	case formatTSV:
		for _, q := range successful {
			answer := strings.ReplaceAll(q.answer, "\n", " ")
			fmt.Printf("%s\t%s\t%s\t[%d tokens, %.2fs, %.1f tok/s]\n", q.question, q.model, answer, q.tokens, q.duration.Seconds(), q.tokensPerSec)
		}
		fmt.Println("Total tokens:", totalTokens)
	default:
		fmt.Fprintf(os.Stderr, "error: unknown format %q (valid: tsv, plain, json)\n", *format)
		os.Exit(1)
	}

	if hadErrors {
		os.Exit(1)
	}
}
