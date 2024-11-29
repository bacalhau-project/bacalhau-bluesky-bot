package main

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

	"github.com/joho/godotenv"
)

var (
	blueskyAPIBase = "https://bsky.social/xrpc"
	username       = ""
	password       = ""
	respondedFile  = "responded_to.txt"
	startTime      time.Time
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
	Text      string `json:"text,omitempty"`
	CreatedAt string `json:"createdAt"`
}

func main() {
	// Load environment variables
	err := godotenv.Load()
	if err != nil {
		fmt.Println("Could not find .env file. Continuing with existing environment variables.")
	}

	username = os.Getenv("BLUESKY_USER")
	password = os.Getenv("BLUESKY_PASS")

	if username == "" || password == "" {
		fmt.Println("Missing environment variables. Please set BLUESKY_USER and BLUESKY_PASS.")
		os.Exit(1)
	}

	// Authenticate with Bluesky API
	session, err := authenticate(username, password)
	if err != nil {
		fmt.Println("Authentication error:", err)
		return
	}

	startTime = time.Now()

	// Poll notifications every 10 seconds
	for {
		fmt.Println("Fetching notifications...")
		notifications, err := fetchNotifications(session.AccessJwt)
		if err != nil {
			fmt.Println("Error fetching notifications:", err)
			time.Sleep(10 * time.Second)
			continue
		}

		for _, notif := range notifications {
			// Process only "mention" notifications
			if notif.Reason == "mention" && shouldRespond(notif) && !hasResponded(notif.Uri) {
				fmt.Printf("Mention detected: %s\n", notif.Record.Text)

				// Respond to the mention
				replyText := "General Kenobi..."
				responseUri, err := replyToMention(session.AccessJwt, notif, replyText, session.Did)
				if err != nil {
					fmt.Println("Error responding to mention:", err)
				} else {
					fmt.Printf("Responded to mention: %s\n", notif.Record.Text)
					recordResponse(notif.Uri)    // Record the original mention
					recordResponse(responseUri) // Record the reply
				}
			}
		}

		time.Sleep(10 * time.Second)
	}
}

func authenticate(username, password string) (*Session, error) {
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

func fetchNotifications(jwt string) ([]Notification, error) {
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

	return notificationResponse.Notifications, nil
}

func replyToMention(jwt string, notif Notification, text string, userDid string) (string, error) {
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

func shouldRespond(notif Notification) bool {

	fmt.Println("Checking whether we should respond based on time:", notif)

	postTime, err := time.Parse(time.RFC3339, notif.Record.CreatedAt)
	if err != nil {
		fmt.Printf("Error parsing timestamp for notification URI %s: %v\n", notif.Uri, err)
		return false
	}

	fmt.Println("\t", postTime.After(startTime), "\n")

	return postTime.After(startTime)
}

func hasResponded(postUri string) bool {
	
	fmt.Println("Checking whether we have responded already:", postUri)

	file, err := os.Open(respondedFile)
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

	fmt.Println("\t", haveWeResponded, "\n")	

	return haveWeResponded
}

func recordResponse(postUri string) {
	file, err := os.OpenFile(respondedFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println("Error opening responded file for writing:", err)
		return
	}
	defer file.Close()

	if _, err := file.WriteString(postUri + "\n"); err != nil {
		fmt.Println("Error writing to responded file:", err)
	}
}
