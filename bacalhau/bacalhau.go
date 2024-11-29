package bacalhau

import(
	"fmt"
	"regexp"
	"strings"
	"io/ioutil"
	"net/http"

	"bbb/bsky"
)

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

	// Convert the response body to a string
	fileContent := string(body)

	return fileContent, nil

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
