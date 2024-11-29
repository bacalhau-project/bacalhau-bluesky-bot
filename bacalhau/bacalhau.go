package bacalhau

import(

	"fmt"
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"io/ioutil"
	"net/http"
	
	"bbb/bsky"

	"gopkg.in/yaml.v3"
)

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

func CreateJob(jobSpec string) error {
	url := fmt.Sprintf("http://%s/api/v1/orchestrator/jobs", BACALHAU_HOST)

	fmt.Println("Sending job to:", url)

	// Convert the job specification string to a JSON byte slice
	jsonData := []byte(jobSpec)

	// Create a new HTTP POST request
	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Send the request using the default HTTP client
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send HTTP request: %v", err)
	}
	defer resp.Body.Close()

	// Check if the request was successful
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to create job, status code: %d", resp.StatusCode)
	}

	// Optionally, decode the response body to get details about the created job
	var response map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("failed to decode response: %v", err)
	}

	fmt.Println("Job created successfully:", response)
	return nil
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
