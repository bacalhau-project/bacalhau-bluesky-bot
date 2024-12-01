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

func CreateJob(jobSpec string) JobExecutionResult {
	url := fmt.Sprintf("http://%s/api/v1/orchestrator/jobs", BACALHAU_HOST)
	fmt.Println("Sending job to:", url)

	// Convert the job specification string to a JSON byte slice
	jsonData := []byte(jobSpec)

	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Printf("Error creating HTTP request: %v\n", err)
		return JobExecutionResult{}
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error sending HTTP request: %v\n", err)
		return JobExecutionResult{}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Failed to create job, status code: %d\n", resp.StatusCode)
		return JobExecutionResult{}
	}

	var response struct {
		JobID string `json:"JobID"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		fmt.Printf("Error decoding job creation response: %v\n", err)
		return JobExecutionResult{}
	}

	if response.JobID == "" {
		fmt.Println("Job creation response missing JobID.")
		return JobExecutionResult{}
	}

	fmt.Printf("Job created successfully with ID: %s\n", response.JobID)

	// Wait for 20 seconds
	fmt.Println("Waiting for 20 seconds before querying executions...")
	time.Sleep(20 * time.Second)

	executionsURL := fmt.Sprintf("http://%s/api/v1/orchestrator/jobs/%s/executions", BACALHAU_HOST, response.JobID)
	req, err = http.NewRequest("GET", executionsURL, nil)
	if err != nil {
		fmt.Printf("Error creating request for executions: %v\n", err)
		return JobExecutionResult{}
	}

	resp, err = client.Do(req)
	if err != nil {
		fmt.Printf("Error fetching executions: %v\n", err)
		return JobExecutionResult{}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Failed to fetch executions, status code: %d\n", resp.StatusCode)
		return JobExecutionResult{}
	}

	var executionsResponse struct {
		Items []struct {
			ID        string `json:"ID"`
			RunOutput struct {
				Stdout string `json:"Stdout"`
			} `json:"RunOutput"`
		} `json:"Items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&executionsResponse); err != nil {
		fmt.Printf("Error decoding executions response: %v\n", err)
		return JobExecutionResult{}
	}

	if len(executionsResponse.Items) == 0 {
		fmt.Printf("No executions found for JobID: %s\n", response.JobID)
		return JobExecutionResult{}
	}

	firstExecution := executionsResponse.Items[0]
	return JobExecutionResult{
		JobID:       response.JobID,
		ExecutionID: firstExecution.ID,
		Stdout:      firstExecution.RunOutput.Stdout,
	}
}


func StopJob(jobID, reason string, wait bool) (string, error) {

	if wait == true {
		fmt.Println("Waiting 40 seconds before killing job with ID:", jobID)
		time.Sleep(40 * time.Second)
	}

	// Construct the URL for stopping the job
	url := fmt.Sprintf("http://%s/api/v1/orchestrator/jobs/%s", BACALHAU_HOST, jobID)

	// Create the payload with the reason
	payload := map[string]string{"reason": reason}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %v", err)
	}

	// Create a new HTTP DELETE request
	req, err := http.NewRequest("DELETE", url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Send the request using the default HTTP client
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send HTTP request: %v", err)
	}
	defer resp.Body.Close()

	// Check if the request was successful
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to stop job, status code: %d", resp.StatusCode)
	}

	// Decode the response body to get the evaluation ID
	var response struct {
		EvaluationID string `json:"EvaluationID"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", fmt.Errorf("failed to decode response: %v", err)
	}

	fmt.Println("Job stopped:", jobID)

	return response.EvaluationID, nil
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

	// Check if the post matches the pattern
	return isCommand, components
}
