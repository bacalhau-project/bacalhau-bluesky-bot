package bsky

import (
	"bufio"
	"bytes"
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
	Uri      string `json:"uri"`
	Cid      string `json:"cid"`
	Author   Author `json:"author"`
	Reason   string `json:"reason"`
	Record   Record `json:"record"`
	IndexedAt string `json:"indexedAt"`
}

type Author struct {
	Handle string `json:"handle"`
}

type Record struct {
	Text      string  `json:"text,omitempty"`
	CreatedAt string  `json:"createdAt"`
	Facets    []Facet `json:"facets,omitempty"`
}

type Facet struct {
	Type     string    `json:"$type"`
	Index    Index     `json:"index"`
	Features []Feature `json:"features"`
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
	processedNotifications := ProcessNotificationText(notificationResponse.Notifications)

	return processedNotifications, nil
}

func ReplyToMention(jwt string, notif Notification, text string, userDid string) (string, error) {
	url := fmt.Sprintf("%s/com.atproto.repo.createRecord", blueskyAPIBase)

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

func ProcessNotificationText(notifications []Notification) []Notification {
	for i, notif := range notifications {
		// Check if the record has facets
		if notif.Record.Text != "" && notif.Record.Facets != nil {
			for _, facet := range notif.Record.Facets {
				// Check for a #link feature
				for _, feature := range facet.Features {
					if feature.Type == "app.bsky.richtext.facet#link" {
						// Replace the text in the record using the facet's URI
						start := facet.Index.ByteStart
						end := facet.Index.ByteEnd

						// Replace the substring in text with the link's URI
						notif.Record.Text = notif.Record.Text[:start] + feature.URI + notif.Record.Text[end:]
					}
				}
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
