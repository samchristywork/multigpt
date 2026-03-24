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
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"
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
	timeout      time.Duration
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
	Context         []int  `json:"context"`
	Done            bool   `json:"done"`
	Error           string `json:"error"`
}

func ask(ollamaURL string, model string, think bool, system string, query string, timeout time.Duration, retries int, ctx []int) (string, int, time.Duration, float64, []int, string) {
	client := &http.Client{Timeout: timeout}
	type payload struct {
		Model   string `json:"model"`
		Prompt  string `json:"prompt"`
		System  string `json:"system"`
		Stream  bool   `json:"stream"`
		Think   bool   `json:"think"`
		Context []int  `json:"context,omitempty"`
	}

	data, err := json.Marshal(payload{
		Model:   model,
		Prompt:  query,
		System:  system,
		Stream:  false,
		Think:   think,
		Context: ctx,
	})
	if err != nil {
		return "", 0, 0, 0, nil, err.Error()
	}

	var lastErr string
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(1<<uint(attempt-1)) * time.Second)
		}

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
			return "", 0, 0, 0, nil, result.Error
		}

		var tokensPerSec float64
		if result.EvalDuration > 0 {
			tokensPerSec = float64(result.EvalCount) / (float64(result.EvalDuration) / 1e9)
		}

		return result.Response, result.EvalCount + result.PromptEvalCount, time.Duration(result.TotalDuration), tokensPerSec, result.Context, ""
	}

	return "", 0, 0, 0, nil, lastErr
}

func askStream(ollamaURL, model string, think bool, system, query string, timeout time.Duration, retries int, ctx []int, w io.Writer) (int, time.Duration, float64, []int, string) {
	client := &http.Client{Timeout: timeout}
	type payload struct {
		Model   string `json:"model"`
		Prompt  string `json:"prompt"`
		System  string `json:"system"`
		Stream  bool   `json:"stream"`
		Think   bool   `json:"think"`
		Context []int  `json:"context,omitempty"`
	}

	data, err := json.Marshal(payload{
		Model:   model,
		Prompt:  query,
		System:  system,
		Stream:  true,
		Think:   think,
		Context: ctx,
	})
	if err != nil {
		return 0, 0, 0, nil, err.Error()
	}

	var lastErr string
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(1<<uint(attempt-1)) * time.Second)
		}

		resp, err := client.Post(ollamaURL+"/api/generate", "application/json", bytes.NewReader(data))
		if err != nil {
			lastErr = err.Error()
			continue
		}

		started := false
		scanner := bufio.NewScanner(resp.Body)
		var scanErr string
		for scanner.Scan() {
			var chunk ollamaResponse
			if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
				scanErr = err.Error()
				break
			}
			if chunk.Error != "" {
				scanErr = chunk.Error
				break
			}
			if !started && chunk.Response != "" {
				started = true
			}
			fmt.Fprint(w, chunk.Response)
			if chunk.Done {
				fmt.Fprintln(w)
				resp.Body.Close()
				var tokensPerSec float64
				if chunk.EvalDuration > 0 {
					tokensPerSec = float64(chunk.EvalCount) / (float64(chunk.EvalDuration) / 1e9)
				}
				return chunk.EvalCount + chunk.PromptEvalCount, time.Duration(chunk.TotalDuration), tokensPerSec, chunk.Context, ""
			}
		}
		resp.Body.Close()
		if scanErr == "" {
			if err := scanner.Err(); err != nil {
				scanErr = err.Error()
			} else {
				scanErr = "stream ended without done message"
			}
		}
		if started {
			return 0, 0, 0, nil, scanErr
		}
		lastErr = scanErr
	}
	return 0, 0, 0, nil, lastErr
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

func printCompletion(shell string) {
	switch shell {
	case "bash":
		fmt.Print(`_multigpt_completion() {
    local cur prev opts
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"
    opts="--role --input --model --think --url --timeout --j --list-models --format --retries --output --context --stream --template --dry-run --completion"
    case "${prev}" in
        --format)   COMPREPLY=( $(compgen -W "plain tsv json" -- "${cur}") ); return ;;
        --completion) COMPREPLY=( $(compgen -W "bash zsh fish" -- "${cur}") ); return ;;
        --input|--output) COMPREPLY=( $(compgen -f -- "${cur}") ); return ;;
    esac
    COMPREPLY=( $(compgen -W "${opts}" -- "${cur}") )
}
complete -F _multigpt_completion multigpt
`)
	case "zsh":
		fmt.Print(`#compdef multigpt
_multigpt() {
    _arguments \
        '--role[System prompt]:role:' \
        '--input[Input file]:file:_files' \
        '--model[Ollama model]:model:' \
        '--think[Enable think mode]' \
        '--url[Ollama server URL]:url:' \
        '--timeout[HTTP timeout in seconds]:seconds:' \
        '-j[Max concurrent requests]:concurrency:' \
        '--list-models[List available models and exit]' \
        '--format[Output format]:format:(plain tsv json)' \
        '--retries[Number of retries]:retries:' \
        '--output[Output file]:file:_files' \
        '--context[Thread context across questions]' \
        '--stream[Stream tokens as they arrive]' \
        '--template[Prompt template]:template:' \
        '--dry-run[Print config and questions without sending requests]' \
        '--completion[Generate shell completion script]:shell:(bash zsh fish)'
}
_multigpt "$@"
`)
	case "fish":
		fmt.Print(`complete -c multigpt -l role       -d 'System prompt'
complete -c multigpt -l input      -d 'Input file' -r -F
complete -c multigpt -l model      -d 'Ollama model'
complete -c multigpt -l think      -d 'Enable think mode'
complete -c multigpt -l url        -d 'Ollama server URL'
complete -c multigpt -l timeout    -d 'HTTP timeout in seconds'
complete -c multigpt -s j          -d 'Max concurrent requests'
complete -c multigpt -l list-models -d 'List available models and exit'
complete -c multigpt -l format     -d 'Output format' -r -a 'plain tsv json'
complete -c multigpt -l retries    -d 'Number of retries'
complete -c multigpt -l output     -d 'Output file' -r -F
complete -c multigpt -l context    -d 'Thread context across questions'
complete -c multigpt -l stream     -d 'Stream tokens as they arrive'
complete -c multigpt -l template   -d 'Prompt template'
complete -c multigpt -l dry-run    -d 'Print config and questions without sending requests'
complete -c multigpt -l completion -d 'Generate shell completion script' -r -a 'bash zsh fish'
`)
	default:
		fmt.Fprintf(os.Stderr, "error: unknown shell %q (valid: bash, zsh, fish)\n", shell)
		os.Exit(1)
	}
}

type config struct {
	Role        string `json:"role"`
	Model       string `json:"model"`
	URL         string `json:"url"`
	TimeoutSecs int    `json:"timeout"`
	Concurrency int    `json:"j"`
	Format      string `json:"format"`
	Retries     int    `json:"retries"`
}

func mergeConfig(base, override config) config {
	if override.Role != "" {
		base.Role = override.Role
	}
	if override.Model != "" {
		base.Model = override.Model
	}
	if override.URL != "" {
		base.URL = override.URL
	}
	if override.TimeoutSecs != 0 {
		base.TimeoutSecs = override.TimeoutSecs
	}
	if override.Concurrency != 0 {
		base.Concurrency = override.Concurrency
	}
	if override.Format != "" {
		base.Format = override.Format
	}
	if override.Retries != 0 {
		base.Retries = override.Retries
	}
	return base
}

func loadConfig() config {
	var candidates []string
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".config", "multigpt", "config.json"))
	}
	candidates = append(candidates, ".multigpt.json")

	var cfg config
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var fileCfg config
		if err := json.Unmarshal(data, &fileCfg); err != nil {
			fmt.Fprintf(os.Stderr, "warning: ignoring malformed config %s: %v\n", path, err)
			continue
		}
		cfg = mergeConfig(cfg, fileCfg)
	}
	return cfg
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
	format := flag.String("format", "plain", "Output format: tsv, plain, or json.")
	retries := flag.Int("retries", 0, "Number of retries on transient errors.")
	outputFile := flag.String("output", "", "Write results to file instead of stdout.")
	conversation := flag.Bool("context", false, "Thread context across questions (sequential, maintains conversation state).")
	stream := flag.Bool("stream", false, "Stream tokens as they arrive (sequential, plain text only).")
	tmplStr := flag.String("template", "", `Go template wrapping each input line, e.g. "Translate to French: {{.}}"`)
	dryRun := flag.Bool("dry-run", false, "Print resolved config and questions without sending any requests.")
	completion := flag.String("completion", "", "Print shell completion script and exit (bash, zsh, fish).")

	flag.Parse()

	cfg := loadConfig()
	explicitly := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { explicitly[f.Name] = true })
	if !explicitly["role"] && cfg.Role != "" {
		*role = cfg.Role
	}
	if !explicitly["model"] && cfg.Model != "" {
		*model = cfg.Model
	}
	if !explicitly["url"] && cfg.URL != "" {
		*ollamaURL = cfg.URL
	}
	if !explicitly["timeout"] && cfg.TimeoutSecs != 0 {
		*timeoutSecs = cfg.TimeoutSecs
	}
	if !explicitly["j"] && cfg.Concurrency != 0 {
		*concurrency = cfg.Concurrency
	}
	if !explicitly["format"] && cfg.Format != "" {
		*format = cfg.Format
	}
	if !explicitly["retries"] && cfg.Retries != 0 {
		*retries = cfg.Retries
	}

	if *completion != "" {
		printCompletion(*completion)
		return
	}

	if *listModelsFlag {
		listModels(*ollamaURL, time.Duration(*timeoutSecs)*time.Second)
		return
	}

	var tmpl *template.Template
	if *tmplStr != "" {
		var err error
		tmpl, err = template.New("prompt").Parse(*tmplStr)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error: invalid template:", err)
			os.Exit(1)
		}
	}

	switch outputFormat(*format) {
	case formatPlain, formatTSV, formatJSON:
	default:
		fmt.Fprintf(os.Stderr, "error: unknown format %q (valid: tsv, plain, json)\n", *format)
		os.Exit(1)
	}

	if *stream && outputFormat(*format) != formatPlain {
		fmt.Fprintln(os.Stderr, "error: --stream requires --format plain")
		os.Exit(1)
	}

	var out io.Writer = os.Stdout
	var outBuf *bytes.Buffer
	if *outputFile != "" {
		if *stream {
			f, err := os.Create(*outputFile)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
				os.Exit(1)
			}
			defer f.Close()
			out = f
		} else {
			outBuf = &bytes.Buffer{}
			out = outBuf
		}
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
	defaultTimeout := time.Duration(*timeoutSecs) * time.Second
	for _, m := range models {
		for _, line := range lines {
			text := line
			qTimeout := defaultTimeout
			if idx := strings.Index(line, ": "); idx > 0 {
				if d, err := time.ParseDuration(line[:idx]); err == nil {
					text = line[idx+2:]
					qTimeout = d
				}
			}
			if tmpl != nil {
				var buf strings.Builder
				if err := tmpl.Execute(&buf, text); err != nil {
					fmt.Fprintln(os.Stderr, "error: template execution failed:", err)
					os.Exit(1)
				}
				text = buf.String()
			}
			questions = append(questions, question{
				question: text,
				model:    m,
				timeout:  qTimeout,
			})
		}
	}

	if *dryRun {
		fmt.Fprintln(os.Stderr, "--- dry run ---")
		for _, q := range questions {
			fmt.Fprintf(os.Stderr, "[%s] [timeout=%s] %s\n", q.model, q.timeout, q.question)
		}
		return
	}

	total := len(questions)
	var done int64
	n := len(lines)

	if *stream {
		hadErrors := false
		for i := range models {
			var ctx []int
			for j := 0; j < n; j++ {
				idx := i*n + j
				q := &questions[idx]
				fmt.Fprintf(os.Stderr, "Q: %s\nM: %s\n", q.question, q.model)
				fmt.Fprintf(out, "Q: %s\nM: %s\nA: ", q.question, q.model)
				var returnedCtx []int
				q.tokens, q.duration, q.tokensPerSec, returnedCtx, q.err = askStream(*ollamaURL, q.model, *think, *role, q.question, q.timeout, *retries, ctx, out)
				if q.err != "" {
					fmt.Fprintf(os.Stderr, "error: %s: %s\n", q.question, q.err)
					hadErrors = true
				}
				if *conversation {
					if q.err != "" {
						ctx = nil
					} else {
						ctx = returnedCtx
					}
				}
				if q.err == "" {
					fmt.Fprintf(out, "   [%d tokens, %.2fs, %.1f tok/s]\n\n", q.tokens, q.duration.Seconds(), q.tokensPerSec)
				}
				d := atomic.AddInt64(&done, 1)
				fmt.Fprintf(os.Stderr, "[%d/%d done]\n", d, total)
			}
		}
		if hadErrors {
			os.Exit(1)
		}
		return
	}

	if *conversation {
		for i := range models {
			var ctx []int
			for j := 0; j < n; j++ {
				idx := i*n + j
				questions[idx].answer, questions[idx].tokens, questions[idx].duration, questions[idx].tokensPerSec, ctx, questions[idx].err = ask(*ollamaURL, questions[idx].model, *think, *role, questions[idx].question, questions[idx].timeout, *retries, ctx)
				if questions[idx].err != "" {
					ctx = nil
				}
				d := atomic.AddInt64(&done, 1)
				fmt.Fprintf(os.Stderr, "[%d/%d done]\n", d, total)
			}
		}
	} else {
		limit := *concurrency
		if limit <= 0 {
			limit = len(lines)
		}
		if limit < 1 {
			limit = 1
		}
		sem := make(chan struct{}, limit)

		for i := range models {
			var wg sync.WaitGroup
			for j := 0; j < n; j++ {
				idx := i*n + j
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()
					questions[idx].answer, questions[idx].tokens, questions[idx].duration, questions[idx].tokensPerSec, _, questions[idx].err = ask(*ollamaURL, questions[idx].model, *think, *role, questions[idx].question, questions[idx].timeout, *retries, nil)
					d := atomic.AddInt64(&done, 1)
					fmt.Fprintf(os.Stderr, "[%d/%d done]\n", d, total)
				}(idx)
			}
			wg.Wait()
		}
	}

	totalTokens := 0
	var totalDuration time.Duration
	failures := 0
	var successful []question
	hadErrors := false
	for _, q := range questions {
		if q.err != "" {
			fmt.Fprintf(os.Stderr, "error: %s: %s\n", q.question, q.err)
			hadErrors = true
			failures++
			continue
		}
		totalTokens += q.tokens
		totalDuration += q.duration
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
		type jsonSummary struct {
			TotalTokens      int     `json:"total_tokens"`
			TotalDurationSecs float64 `json:"total_duration_secs"`
			Succeeded        int     `json:"succeeded"`
			Failed           int     `json:"failed"`
		}
		envelope := struct {
			Results []jsonQuestion `json:"results"`
			Summary jsonSummary    `json:"summary"`
		}{
			Results: results,
			Summary: jsonSummary{totalTokens, totalDuration.Seconds(), len(successful), failures},
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		enc.Encode(envelope)
	case formatPlain:
		for _, q := range successful {
			fmt.Fprintf(out, "Q: %s\nM: %s\nA: %s\n   [%d tokens, %.2fs, %.1f tok/s]\n\n", q.question, q.model, q.answer, q.tokens, q.duration.Seconds(), q.tokensPerSec)
		}
		fmt.Fprintln(out, "Total tokens:", totalTokens)
	case formatTSV:
		for _, q := range successful {
			question := strings.ReplaceAll(q.question, "\t", " ")
			answer := strings.ReplaceAll(q.answer, "\n", " ")
			fmt.Fprintf(out, "%s\t%s\t%s\t[%d tokens, %.2fs, %.1f tok/s]\n", question, q.model, answer, q.tokens, q.duration.Seconds(), q.tokensPerSec)
		}
		fmt.Fprintln(os.Stderr, "Total tokens:", totalTokens)
	default:
		fmt.Fprintf(os.Stderr, "error: unknown format %q (valid: tsv, plain, json)\n", *format)
		os.Exit(1)
	}

	if outBuf != nil {
		f, err := os.Create(*outputFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		if _, err := f.Write(outBuf.Bytes()); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			f.Close()
			os.Exit(1)
		}
		f.Close()
	}

	if hadErrors {
		os.Exit(1)
	}
}
