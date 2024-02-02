package main

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func calculateCost(tokens float64) float64 {
	tokensPerDollar := 28476.0
	return tokens / tokensPerDollar
}

func getDateTime() string {
	currentTime := time.Now()
	return currentTime.Format("2006-01-02 15:04:05")
}

func getKey() string {
	filename := ".env"
	file, err := os.Open(filename)
	if err != nil {
		fmt.Println("Error: ", err)
		os.Exit(1)
	}

	defer file.Close()

	var key string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key = scanner.Text()
	}

	return key
}

func ask(role string, query string) (string, float64) {
	api_key := getKey()
	url := "https://api.openai.com/v1/chat/completions"
	method := "POST"
	headers := map[string]string{
		"Authorization": "Bearer " + api_key,
		"Content-Type":  "application/json",
	}

	payload := fmt.Sprintf("{\"model\": \"gpt-4\", \"messages\": [{\"role\": \"system\", \"content\": \"%s\"}, {\"role\": \"user\", \"content\": \"%s\"}]}", role, query)

	client := &http.Client{}
	req, err := http.NewRequest(method, url, strings.NewReader(payload))
	if err != nil {
		fmt.Println(err)
		return "error", 0
	}

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	res, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return "error", 0
	}

	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		fmt.Println(err)
		return "error", 0
	}

	var result map[string]interface{}
	json.Unmarshal([]byte(body), &result)
	tokens := result["usage"].(map[string]interface{})["total_tokens"].(float64)
	choices := result["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})
	text := choice["message"].(map[string]interface{})["content"].(string)
	text = strings.ReplaceAll(text, "\n", " ")
	return text, tokens
}
