package bacalhau

import(

	"fmt"
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"io/ioutil"
	"net/http"
	"time"
	
	"bbb/bsky"

	"gopkg.in/yaml.v3"
)

type JobExecutionResult struct {
	JobID        string `json:"JobID"`
	ExecutionID  string `json:"ExecutionID"`
	Stdout       string `json:"Stdout"`
}

var BACALHAU_HOST string

func GetLinkedJobFile(url string) (string, error) {

	fmt.Println("Getting job file from URL:", url)

	// Make a GET request to the provided URL
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch file from URL: %v", err)
	}
	defer resp.Body.Close()

	// Check if the response status code is successful
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to fetch file: status code %d", resp.StatusCode)
	}

	// Read the response body
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read file content: %v", err)
	}

	// Parse the YAML content into a generic map
	var yamlContent map[string]interface{}
	err = yaml.Unmarshal(body, &yamlContent)
	if err != nil {
		return "", fmt.Errorf("failed to parse YAML: %v", err)
	}

	// Nest the parsed YAML map under the "Job" property
	wrappedContent := map[string]interface{}{
		"Job": yamlContent,
	}

	// Convert the wrapped content to JSON
	jsonContent, err := json.MarshalIndent(wrappedContent, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to convert wrapped content to JSON: %v", err)
	}

	// Return the formatted JSON string
	return string(jsonContent), nil
}

func CreateJob(jobSpec string) (*JobExecutionResult, error) {
	// Endpoint to submit the job
	url := fmt.Sprintf("http://%s/api/v1/orchestrator/jobs", BACALHAU_HOST)

	fmt.Println("Sending job to:", url)

	// Convert the job specification string to a JSON byte slice
	jsonData := []byte(jobSpec)

	// Create a new HTTP POST request
	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Send the request using the default HTTP client
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send HTTP request: %v", err)
	}
	defer resp.Body.Close()

	// Check if the request was successful
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to create job, status code: %d", resp.StatusCode)
	}

	// Decode the response body to get the job details
	var response struct {
		JobID string `json:"JobID"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	fmt.Printf("Job created successfully with ID: %s\n", response.JobID)

	// Wait for 20 seconds
	fmt.Println("Waiting for 20 seconds before querying executions...")
	time.Sleep(20 * time.Second)

	// Query the executions endpoint
	executionsURL := fmt.Sprintf("http://%s/api/v1/orchestrator/jobs/%s/executions", BACALHAU_HOST, response.JobID)
	req, err = http.NewRequest("GET", executionsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for executions: %v", err)
	}

	resp, err = client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch executions: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch executions, status code: %d", resp.StatusCode)
	}

	// Decode the executions response
	var executionsResponse struct {
		Items []struct {
			ID         string `json:"ID"`
			RunOutput  struct {
				Stdout string `json:"Stdout"`
			} `json:"RunOutput"`
		} `json:"Items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&executionsResponse); err != nil {
		return nil, fmt.Errorf("failed to decode executions response: %v", err)
	}

	// Extract the first execution result
	if len(executionsResponse.Items) == 0 {
		return nil, fmt.Errorf("no executions found for JobID: %s", response.JobID)
	}
	firstExecution := executionsResponse.Items[0]

	// Populate the result struct
	result := &JobExecutionResult{
		JobID:       response.JobID,
		ExecutionID: firstExecution.ID,
		Stdout:      firstExecution.RunOutput.Stdout,
	}

	return result, nil
}

func CheckPostIsCommand(post string, accountUsername string) (bool, bsky.PostComponents) {
	// Define the regex pattern to validate the command structure
	pattern := `^@` + regexp.QuoteMeta(accountUsername) + `\s+job\s+run\s+https?://\S+$`

	// Compile the regex
	re := regexp.MustCompile(pattern)

	isCommand := re.MatchString(post)

	components := bsky.PostComponents{}

	if isCommand {
		// Split the post string into parts
		parts := strings.Fields(post)

		if len(parts) >= 4 { // Ensure the post has at least 4 parts
			components.Text = post
			components.Url = parts[3] // Assign the 4th part as the URL
		}
	}

	fmt.Println("components:", components)

	// Check if the post matches the pattern
	return isCommand, components
}
