package bsky

import (
	"bufio"
	"bytes"
	"regexp"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"
)

type Session struct {
	AccessJwt string `json:"accessJwt"`
	Did       string `json:"did"`
}

type NotificationResponse struct {
	Notifications []Notification `json:"notifications"`
}

type Notification struct {
	Uri       string `json:"uri"`
	Cid       string `json:"cid"`
	Author    Author `json:"author"`
	Reason    string `json:"reason"`
	Record    Record `json:"record"`
	IndexedAt string `json:"indexedAt"`
	ImageURL  string `json:"imageURL"`
}

type Author struct {
	Handle string `json:"handle"`
	Did    string `json:"did"`
}

type Record struct {
	Text      string  `json:"text,omitempty"`
	CreatedAt string  `json:"createdAt"`
	Facets    []Facet `json:"facets,omitempty"`
	Embed     *Embed  `json:"embed,omitempty"` // Change from []Embed to *Embed
}

type Facet struct {
	Type     string    `json:"$type"`
	Index    Index     `json:"index"`
	Features []Feature `json:"features"`
}

type Embed struct {
	Type   string  `json:"$type"`
	Images []Image `json:"images,omitempty"`
}

type Image struct {
	Alt         string   `json:"alt"`
	AspectRatio struct {
		Height int `json:"height"`
		Width  int `json:"width"`
	} `json:"aspectRatio"`
	Image struct {
		Type     string            `json:"$type"`
		Ref      map[string]string `json:"ref"`
		MimeType string            `json:"mimeType"`
		Size     int               `json:"size"`
	} `json:"image"`
}

type Feature struct {
	Type string `json:"$type"`
	URI  string `json:"uri,omitempty"` // For #link features
}

type Index struct {
	ByteStart int `json:"byteStart"`
	ByteEnd   int `json:"byteEnd"`
}

type PostComponents struct {
	Text string
	Url string
}

var Username string
var Password string
var blueskyAPIBase = "https://bsky.social/xrpc"
var StartTime time.Time
var RespondedFile = "responded_to.txt"

func Authenticate(username, password string) (*Session, error) {
	url := fmt.Sprintf("%s/com.atproto.server.createSession", blueskyAPIBase)
	body := fmt.Sprintf(`{"identifier": "%s", "password": "%s"}`, username, password)

	req, err := http.NewRequest("POST", url, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to authenticate, status code: %d", resp.StatusCode)
	}

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var session Session
	if err := json.Unmarshal(respBody, &session); err != nil {
		return nil, err
	}

	return &session, nil
}

func FetchNotifications(jwt string) ([]Notification, error) {
	url := fmt.Sprintf("%s/app.bsky.notification.listNotifications", blueskyAPIBase)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch notifications, status code: %d", resp.StatusCode)
	}

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var notificationResponse NotificationResponse
	if err := json.Unmarshal(respBody, &notificationResponse); err != nil {
		return nil, err
	}

	// Print raw response for debugging
	// fmt.Println("Raw:", string(respBody))

	// Process notifications to update record text based on facets
	processedNotifications := ProcessNotifications(notificationResponse.Notifications)

	return processedNotifications, nil
}

func UploadImage(jwt string, imageData []byte) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/com.atproto.repo.uploadBlob", blueskyAPIBase)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(imageData))
	if err != nil {
		return nil, fmt.Errorf("failed to create image upload request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Content-Type", "image/jpeg") // Adjust for other formats like "image/png" if needed

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("image upload failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("image upload failed, status code: %d, response: %s", resp.StatusCode, string(respBody))
	}

	var response map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image upload response: %v", err)
	}

	blob, ok := response["blob"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("image upload response missing 'blob' field or it is not a map")
	}

	return blob, nil
}

func ReplyToMentionWithImage(jwt string, notif Notification, text string, imageData []byte, userDid string) (string, error) {
	// Step 1: Upload the image
	imageBlob, err := UploadImage(jwt, imageData)
	if err != nil {
		return "", fmt.Errorf("failed to upload image: %v", err)
	}

	// Step 2: Prepare the payload for replying with the image
	url := fmt.Sprintf("%s/com.atproto.repo.createRecord", blueskyAPIBase)

	payload := map[string]interface{}{
		"collection": "app.bsky.feed.post",
		"repo":       userDid,
		"record": map[string]interface{}{
			"$type":     "app.bsky.feed.post",
			"text":      text,
			"createdAt": time.Now().Format(time.RFC3339),
			"reply": map[string]interface{}{
				"root": map[string]string{
					"uri": notif.Uri,
					"cid": notif.Cid,
				},
				"parent": map[string]string{
					"uri": notif.Uri,
					"cid": notif.Cid,
				},
			},
			"embed": map[string]interface{}{
				"$type": "app.bsky.embed.images",
				"images": []map[string]interface{}{
					{
						"image": imageBlob, // Directly use the blob as the image
						"alt":   "Uploaded image", // Provide a meaningful alt description if needed
					},
				},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %v", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := ioutil.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to post reply, status code: %d, response: %s", resp.StatusCode, string(respBody))
	}

	var response map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		return "", fmt.Errorf("failed to decode response: %v", err)
	}

	if uri, ok := response["uri"].(string); ok {
		return uri, nil
	}

	return "", fmt.Errorf("response URI not found")
}


func ReplyToMention(jwt string, notif Notification, text string, userDid string) (string, error) {
	url := fmt.Sprintf("%s/com.atproto.repo.createRecord", blueskyAPIBase)

	// Initialize facets as an empty slice
	var facets []map[string]interface{}

	// Use a regex to find all URLs in the text
	urlRegex := `https?://[^\s]+`
	re := regexp.MustCompile(urlRegex)
	matches := re.FindAllStringIndex(text, -1)

	// Create a facet for each URL
	for _, match := range matches {
		byteStart, byteEnd := match[0], match[1]
		facets = append(facets, map[string]interface{}{
			"index": map[string]int{
				"byteStart": byteStart,
				"byteEnd":   byteEnd,
			},
			"features": []map[string]string{
				{
					"$type": "app.bsky.richtext.facet#link",
					"uri":   text[byteStart:byteEnd],
				},
			},
		})
	}

	// Construct the payload with optional facets
	payload := map[string]interface{}{
		"collection": "app.bsky.feed.post",
		"repo":       userDid, // Use the authenticated user's DID
		"record": map[string]interface{}{
			"$type":     "app.bsky.feed.post",
			"text":      text,
			"createdAt": time.Now().Format(time.RFC3339),
			"reply": map[string]interface{}{
				"root": map[string]string{
					"uri": notif.Uri,
					"cid": notif.Cid,
				},
				"parent": map[string]string{
					"uri": notif.Uri,
					"cid": notif.Cid,
				},
			},
		},
	}

	// Add facets only if they exist
	if len(facets) > 0 {
		payload["record"].(map[string]interface{})["facets"] = facets
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %v", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := ioutil.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to post reply, status code: %d, response: %s", resp.StatusCode, string(respBody))
	}

	var response map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		return "", fmt.Errorf("failed to decode response: %v", err)
	}

	if uri, ok := response["uri"].(string); ok {
		return uri, nil
	}

	return "", fmt.Errorf("response URI not found")
}


func ProcessNotifications(notifications []Notification) []Notification {
	
	for i, notif := range notifications {

		if notif.Record.Text != "" && notif.Record.Facets != nil {

			for _, facet := range notif.Record.Facets {

				for _, feature := range facet.Features {

					if feature.Type == "app.bsky.richtext.facet#link" {
						start := facet.Index.ByteStart
						end := facet.Index.ByteEnd

						// Replace the substring in text with the link's URI
						notif.Record.Text = notif.Record.Text[:start] + feature.URI + notif.Record.Text[end:]
					}

				}

			}

		}

		if notif.Record.Embed != nil && notif.Record.Embed.Type == "app.bsky.embed.images" {

			if len(notif.Record.Embed.Images) > 0 {

				firstImage := notif.Record.Embed.Images[0] // Access the first image
				imageRef := firstImage.Image.Ref["$link"]

				// Construct the image URL
				imageURL := fmt.Sprintf(
					"https://cdn.bsky.app/img/feed_thumbnail/plain/%s/%s@jpeg",
					notif.Author.Did,
					imageRef,
				)

				// Add the generated image URL to the notification
				notif.ImageURL = imageURL

			}

		}

		// Update the notification in the list
		notifications[i] = notif
	}

	return notifications

}

func ShouldRespond(notif Notification) bool {
	postTime, err := time.Parse(time.RFC3339, notif.Record.CreatedAt)
	if err != nil {
		fmt.Printf("Error parsing timestamp for notification URI %s: %v\n", notif.Uri, err)
		return false
	}

	return postTime.After(StartTime)
}

func HasResponded(postUri string) bool {
	file, err := os.Open(RespondedFile)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
		fmt.Println("Error opening responded file:", err)
		return false
	}
	defer file.Close()

	haveWeResponded := false

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if scanner.Text() == postUri {
			haveWeResponded = true
		}
	}

	return haveWeResponded
}

func RecordResponse(postUri string) {
	file, err := os.OpenFile(RespondedFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println("Error opening responded file for writing:", err)
		return
	}
	defer file.Close()

	if _, err := file.WriteString(postUri + "\n"); err != nil {
		fmt.Println("Error writing to responded file:", err)
	}
}
