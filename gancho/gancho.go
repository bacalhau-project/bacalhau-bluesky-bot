package gancho

import (
	"os"
	"fmt"
	"bytes"
	"errors"
	"net/http"
	"io/ioutil"
	"encoding/json"
)

func GenerateShortURL(targetURL string) (string, error) {
	// Gancho API endpoint
	apiEndpoint := os.Getenv("GANCHO_ENDPOINT")

	if apiEndpoint == ""{
		apiEndpoint = "https://go.cod.dev"
	}

	// Authorization token
	apiKey := os.Getenv("GANCHO_KEY")

	if apiKey == ""{
		return "", errors.New("No GANCHO_KEY environment variable is set. Can't authenticate with Gancho to create the shortlink.")
	}

	// Prepare the request payload
	payload := map[string]string{
		"url": targetURL,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("Failed to marshal payload for Gancho: %w", err)
	}

	// Create the HTTP request
	req, err := http.NewRequest("POST", apiEndpoint, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return "", fmt.Errorf("Failed to create Gancho request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", apiKey)

	// Execute the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Gancho request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read and parse the response
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		return "", fmt.Errorf("Request to Gancho failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var responseBody map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&responseBody)
	if err != nil {
		return "", fmt.Errorf("Could not decode Gancho response body: %w", err)
	}

	// Extract the identifier
	identifier, ok := responseBody["identifier"].(string)
	if !ok {
		return "", fmt.Errorf("Unexpected response format from Gancho: missing 'identifier' in response from Gancho")
	}

	// Construct the short URL
	shortURL := fmt.Sprintf("%s/%s", apiEndpoint, identifier)

	return shortURL, nil
}
