package bacalhau

import(
	"os"
	"fmt"
	"bytes"
	"io"
	"time"
	"regexp"
	"strings"
	"errors"
	"encoding/json"
	"encoding/base64"
	"io/ioutil"
	"net/http"
	
	"bbb/bsky"

	"gopkg.in/yaml.v3"
)

type JobExecutionResult struct {
	JobID        string `json:"JobID"`
	ExecutionID  string `json:"ExecutionID"`
	Stdout       string `json:"Stdout"`
}

var BACALHAU_HOST string

func constructOrchestratorURL() (string, error){

	var protocol string
	var hostName string
	var portNumber string

	if os.Getenv("USING_SECURE_ORCHESTRATOR") == "true"{
		protocol = "https"
	} else {
		protocol = "http"
	}

	if os.Getenv("BACALHAU_HOST") == "" {
		return "", errors.New("BACALHAU_HOST is not set with environment variables. Cannot construct Orchestrator URL.")
	} else {
		hostName = os.Getenv("BACALHAU_HOST")
	}

	if os.Getenv("BACALHAU_PORT") == ""{
		fmt.Println("Warning: BACALHAU_PORT is not set with environment variables. Defaulting to 1234")
		portNumber = "1234"
	} else {
		portNumber = os.Getenv("BACALHAU_PORT")
	}

	constructedURL := fmt.Sprintf(`%s://%s:%s`, protocol, hostName, portNumber)

	return constructedURL, nil

}

func getSignedAuthToken() (string, error) {

	accessToken := os.Getenv("BACALHAU_ACCESS_TOKEN")
	if accessToken == "" {
		return "", errors.New("BACALHAU_ACCESS_TOKEN isn't set by environment variables. Cannot generate auth token.")
	}

	b64Token := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf(`{"token":"%s"}`, accessToken)))

	authPayload := map[string]string{
		"MethodData": b64Token,
	}

	payloadBytes, err := json.Marshal(authPayload)
	if err != nil {
		return "", fmt.Errorf("failed to encode auth payload: %v", err)
	}

	orchestratorURL, err := constructOrchestratorURL()
	if err != nil {
		return "", fmt.Errorf("failed to construct orchestrator URL: %v", err)
	}

	authEndpoint := fmt.Sprintf("%s/api/v1/auth/shared_secret", orchestratorURL)

	fmt.Println("Authenticating with orchestrator...")

	req, err := http.NewRequest("POST", authEndpoint, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to authenticate with orchestrator: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to authenticate: status %d, response: %s", resp.StatusCode, string(body))
	}

	var authResponse struct {
		Authentication struct {
			Token string `json:"token"`
		} `json:"Authentication"`
	}

	err = json.NewDecoder(resp.Body).Decode(&authResponse)
	if err != nil {
		return "", fmt.Errorf("failed to parse authentication response: %v", err)
	}

	return authResponse.Authentication.Token, nil

}

func GetJobFileFromURL(url string) (string, error) {

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

func GenerateClassificationJob(imageURL string, isHotDogJob bool, className string) (string, error) {
	// Read the YAML file
	jobFileTemplate, jtErr := os.ReadFile("./classify_job.yaml")
	if jtErr != nil {
		return "", fmt.Errorf("an error occurred reading the classify job.yaml file: %w", jtErr)
	}

	// Parse the YAML into a generic map
	var yamlContent map[string]interface{}
	if err := yaml.Unmarshal(jobFileTemplate, &yamlContent); err != nil {
		return "", fmt.Errorf("an error occurred parsing the YAML file: %w", err)
	}

	// Add or update the IMAGE environment variable manually
	tasks := yamlContent["Tasks"].([]interface{})
	firstTask := tasks[0].(map[string]interface{})
	engine := firstTask["Engine"].(map[string]interface{})
	params := engine["Params"].(map[string]interface{})
	envVars := []string{
		fmt.Sprintf("IMAGE=%s", imageURL),
		fmt.Sprintf("MODEL=%s", os.Getenv("CLASSIFICATION_IMAGE")),
		fmt.Sprintf("AWS_ACCESS_KEY_ID=%s", os.Getenv("AWS_ACCESS_KEY_ID")),
		fmt.Sprintf("AWS_SECRET_ACCESS_KEY=%s", os.Getenv("AWS_SECRET_ACCESS_KEY")),
		fmt.Sprintf("S3_BUCKET=%s", os.Getenv("S3_IMAGE_BUCKET")),
	}

	if isHotDogJob == true {
		envVars = append(envVars, fmt.Sprintf("HOTDOG_DETECTION=%s", "true"),)
	} else {
		envVars = append(envVars, fmt.Sprintf("HOTDOG_DETECTION=%s", "false"),)
	}

	if className != "" {
		envVars = append(envVars, "ARBITRARY_DETECTION=true" )
		envVars = append(envVars, fmt.Sprintf("CLASS_NAME=%s", className) )
	}

	params["EnvironmentVariables"] = envVars

	fmt.Println("TASKS:", tasks)

	// Wrap the updated YAML content into the final JSON structure
	wrappedContent := map[string]interface{}{
		"Job": yamlContent,
	}

	// Convert the map to JSON
	jsonContent, err := json.MarshalIndent(wrappedContent, "", "  ")
	if err != nil {
		return "", fmt.Errorf("an error occurred converting YAML to JSON: %w", err)
	}

	// Return the JSON string
	return string(jsonContent), nil

}

func GenerateAltTextJob(imageURL, prompt string) (string, error) {

	jobFileTemplate, jtErr := os.ReadFile("./alt_text_job.yaml")
	if jtErr != nil {
		return "", fmt.Errorf("an error occurred reading the alt-text job.yaml file: %w", jtErr)
	}

	// Parse the YAML into a generic map
	var yamlContent map[string]interface{}
	if err := yaml.Unmarshal(jobFileTemplate, &yamlContent); err != nil {
		return "", fmt.Errorf("an error occurred parsing the YAML file: %w", err)
	}

	// Add or update the IMAGE environment variable manually
	tasks := yamlContent["Tasks"].([]interface{})
	firstTask := tasks[0].(map[string]interface{})
	engine := firstTask["Engine"].(map[string]interface{})
	params := engine["Params"].(map[string]interface{})
	envVars := []string{
		fmt.Sprintf("IMAGE_URL=%s", imageURL),
		fmt.Sprintf("AWS_ACCESS_KEY_ID=%s", os.Getenv("AWS_ACCESS_KEY_ID")),
		fmt.Sprintf("AWS_SECRET_ACCESS_KEY=%s", os.Getenv("AWS_SECRET_ACCESS_KEY")),
		fmt.Sprintf("S3_BUCKET=%s", os.Getenv("S3_IMAGE_BUCKET")),
		fmt.Sprintf("PROMPT_TEXT=%s", prompt),
	}

	params["EnvironmentVariables"] = envVars

	// Wrap the updated YAML content into the final JSON structure
	wrappedContent := map[string]interface{}{
		"Job": yamlContent,
	}

	// Convert the map to JSON
	jsonContent, err := json.MarshalIndent(wrappedContent, "", "  ")
	if err != nil {
		return "", fmt.Errorf("an error occurred converting YAML to JSON: %w", err)
	}

	// Return the JSON string
	return string(jsonContent), nil

}

func GetResultsForJob(jobID string) (JobExecutionResult, error) {

	var token string
	var tokenErr error

	orchestratorURL, orchErr := constructOrchestratorURL()

	if orchErr != nil {
		fmt.Println("Could not create Job:", orchErr.Error())
		return JobExecutionResult{}, orchErr
	}

	if os.Getenv("USING_SECURE_ORCHESTRATOR") == "true" {

		token, tokenErr = getSignedAuthToken()

		if tokenErr != nil {

			fmt.Println("Could not create Job:", tokenErr.Error())
			return JobExecutionResult{}, tokenErr

		}

	}

	executionsURL := fmt.Sprintf("%s/api/v1/orchestrator/jobs/%s/executions", orchestratorURL, jobID)
	fmt.Println("executionsURL:", executionsURL)

	req, reqErr := http.NewRequest("GET", executionsURL, nil)
	if reqErr != nil {
		fmt.Printf("Error creating request for executions: %v\n", reqErr)
		return JobExecutionResult{}, errors.New(fmt.Sprintf(`Error creating request for executions: %s`, reqErr.Error()))
	}

	if os.Getenv("USING_SECURE_ORCHESTRATOR") == "true" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token) )
	}

	client := &http.Client{}
	resp, respErr := client.Do(req)
	if respErr != nil {
		fmt.Printf("Error fetching executions: %v\n", respErr)
		return JobExecutionResult{}, errors.New(fmt.Sprintf(`Error fetching executions: %s`, respErr.Error()))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Failed to fetch executions, status code: %d\n", resp.StatusCode)
		return JobExecutionResult{}, errors.New(fmt.Sprintf(`Failed to fetch executions (HTTP status code: %d)`, resp.StatusCode))
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
		return JobExecutionResult{}, errors.New(fmt.Sprintf(`Error decoding executions response: %s`, err.Error()))
	}

	if len(executionsResponse.Items) == 0 {
		fmt.Printf("No executions found for JobID: %s\n", jobID)
		return JobExecutionResult{}, errors.New(fmt.Sprintf(`No executions found for JobID "%s"`, jobID))
	}

	chosenJobToReturn := JobExecutionResult{
		JobID: jobID,
	}

	for _, thisExecution := range executionsResponse.Items {
		if thisExecution.RunOutput.Stdout != "" {
			chosenJobToReturn.ExecutionID = thisExecution.ID
			chosenJobToReturn.Stdout = thisExecution.RunOutput.Stdout
		}
	}

	return chosenJobToReturn, nil

}

func CreateJob(jobSpec string, timeToWaitForResults int) JobExecutionResult {

	var token string
	var tokenErr error

	orchestratorURL, orchErr := constructOrchestratorURL()

	if orchErr != nil {
		fmt.Println("Could not create Job:", orchErr.Error())
		return JobExecutionResult{}
	}

	if os.Getenv("USING_SECURE_ORCHESTRATOR") == "true" {

		token, tokenErr = getSignedAuthToken()

		if tokenErr != nil {

			fmt.Println("Could not create Job:", tokenErr.Error())
			return JobExecutionResult{}

		}

	}

	createJobURL := fmt.Sprintf("%s/api/v1/orchestrator/jobs", orchestratorURL)
	fmt.Println("Sending job to:", createJobURL)

	// Convert the job specification string to a JSON byte slice
	jsonData := []byte(jobSpec)

	req, err := http.NewRequest("PUT", createJobURL, bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Printf("Error creating HTTP request: %v\n", err)
		return JobExecutionResult{}
	}
	req.Header.Set("Content-Type", "application/json")
	
	if os.Getenv("USING_SECURE_ORCHESTRATOR") == "true" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token) )
	}

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

	// Wait for a given period before retrieving results...
	fmt.Println(fmt.Sprintf(`Waiting for %d seconds before querying executions for Job "%s"...`, timeToWaitForResults, response.JobID))
	waitTime := time.Duration(timeToWaitForResults) * time.Second
	time.Sleep(waitTime)

	// result, resultErr := GetResultsForJob(response.JobID)

	var result JobExecutionResult
	var resultErr error

	for i := 0; i < 5; i++ {

		fmt.Println(fmt.Printf(`Making attempt no. %d to retrieve results for JobID "%s".`, i + 1, response.JobID))

		result, resultErr = GetResultsForJob(response.JobID)

		if result.JobID != "" {
			break
		} else {
			fmt.Println(fmt.Printf(`Did not get results for JobID "%s".`, response.JobID))
			time.Sleep(waitTime)
		}

	}

	if resultErr != nil {
		fmt.Println( fmt.Sprintf(`Failed to get results for JobID "%s": %s`, response.JobID, resultErr) )
		go StopJob(response.JobID, "Failed to get results in allotted timeframe.", false)
		return JobExecutionResult{}
	}

	return result

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

func CheckPostIsCommand(post string, accountUsername string) (bool, bsky.PostComponents, string, string) {
	var commandType, className string

	// Define the regex patterns
	jobRunPattern := `^@` + regexp.QuoteMeta(accountUsername) + `\s+job\s+run\s+https?://\S+$`
	classifyJobPattern := `^@` + regexp.QuoteMeta(accountUsername) + `\s+classify`
	hotDogDetectionJobPattern := `^@` + regexp.QuoteMeta(accountUsername) + `\s+hotdog?`
	arbitraryClassPattern := `^@` + regexp.QuoteMeta(accountUsername) + `\s+(\w+)\?$`

	// Compile the regex
	jobRunRegex := regexp.MustCompile(jobRunPattern)
	classifyJobRegex := regexp.MustCompile(classifyJobPattern)
	hotDogJobRegex := regexp.MustCompile(hotDogDetectionJobPattern)
	arbitraryClassRegex := regexp.MustCompile(arbitraryClassPattern)

	// Check if the post matches any command pattern
	isJobRunCommand := jobRunRegex.MatchString(post)
	isClassifyJobCommand := classifyJobRegex.MatchString(post)
	isHotDogJobCommand := hotDogJobRegex.MatchString(post)
	isArbitraryClassCommand := arbitraryClassRegex.MatchString(post)
	isAltTextCommand := false

	if strings.Contains(accountUsername, "alt-text.bots.bacalhau.org") {
		isAltTextCommand = true
	}

	components := bsky.PostComponents{}
	parts := strings.Fields(post)
	components.Text = post

	if isJobRunCommand {
		if len(parts) >= 4 { // Ensure the post has at least 4 parts
			components.Url = parts[3] // Assign the 4th part as the URL
		}
		commandType = "job_file"
	}

	if isClassifyJobCommand {
		commandType = "classify_image"
	}

	if isHotDogJobCommand {
		commandType = "hotdog"
	}

	if isArbitraryClassCommand {
		commandType = "arbitraryClass"
		matches := arbitraryClassRegex.FindStringSubmatch(post)
		if len(matches) > 1 {
			className = matches[1] // Extracts <CLASS_NAME>
		}
	}

	if isAltTextCommand {
		commandType = "altText"
	}

	// Check if the post matches any of the patterns
	return isJobRunCommand || isClassifyJobCommand || isHotDogJobCommand || isArbitraryClassCommand || isAltTextCommand, components, commandType, className

}


