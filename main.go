package main

import (
	"io"
	"os"
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
