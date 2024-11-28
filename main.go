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
	blueskyAPIBase   = "https://bsky.social/xrpc"
	username         = ""
	password         = ""
	targetUserHandle = ""
	respondedFile    = "responded_to.txt"
)

type Session struct {
	AccessJwt string `json:"accessJwt"`
	Did       string `json:"did"`
}

type TimelineResponse struct {
	Feed []FeedItem `json:"feed"`
}

type FeedItem struct {
	Post Post `json:"post"`
}

type Post struct {
	Uri         string  `json:"uri"`
	Cid         string  `json:"cid"`
	Author      Author  `json:"author"`
	Record      Record  `json:"record"`
	ReplyCount  int     `json:"replyCount"`
	RepostCount int     `json:"repostCount"`
	LikeCount   int     `json:"likeCount"`
	IndexedAt   string  `json:"indexedAt"`
	Labels      []Label `json:"labels,omitempty"`
}

type Author struct {
	Did         string `json:"did"`
	Handle      string `json:"handle"`
	DisplayName string `json:"displayName,omitempty"`
	Avatar      string `json:"avatar,omitempty"`
	CreatedAt   string `json:"createdAt"`
}

type Record struct {
	Type      string   `json:"$type"`
	CreatedAt string   `json:"createdAt"`
	Langs     []string `json:"langs"`
	Text      string   `json:"text,omitempty"`
}

type Label struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func main() {
	// Load environment variables
	err := godotenv.Load()
	if err != nil {
		fmt.Println("Could not find .env file. Continuing with existing environment variables.")
	}

	username = os.Getenv("BLUESKY_USER")
	password = os.Getenv("BLUESKY_PASS")
	targetUserHandle = os.Getenv("BLUESKY_TARGET_ACCOUNT")

	if username == "" || password == "" || targetUserHandle == "" {
		fmt.Println("Missing environment variables. Please set BLUESKY_USER, BLUESKY_PASS, and BLUESKY_TARGET_ACCOUNT.")
		os.Exit(1)
	}

	// Authenticate with Bluesky API
	session, err := authenticate(username, password)
	if err != nil {
		fmt.Println("Authentication error:", err)
		return
	}

	// Track the start time of the program
	startTime := time.Now()

	// Start polling for new posts
	for {
		fmt.Printf("Getting posts for %s...\n", targetUserHandle)
		feedItems, err := fetchTimeline(session.AccessJwt, session.Did)
		if err != nil {
			fmt.Println("Error fetching timeline:", err)
			time.Sleep(10 * time.Second)
			continue
		}

		for _, item := range feedItems {
			post := item.Post

			// Check if post mentions the target account, has not been responded to, and isn't authored by this script
			if shouldRespond(post, startTime) && !hasResponded(post.Uri) {
				fmt.Println("Responding to post", post)
				replyText := fmt.Sprintf("Hi, %s, you mentioned @%s: %s", post.Author.Handle, targetUserHandle, post.Record.Text)
				responseUri, err := replyToPost(session.AccessJwt, session.Did, post.Uri, post.Cid, replyText)
				if err != nil {
					fmt.Println("Error replying to post:", err)
				} else {
					fmt.Printf("Responded to post: %s\n", post.Record.Text)
					recordResponse(post.Uri)         // Track the original post
					recordResponse(responseUri)      // Track the response
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

func fetchTimeline(jwt, did string) ([]FeedItem, error) {
	url := fmt.Sprintf("%s/app.bsky.feed.getAuthorFeed?actor=%s", blueskyAPIBase, did)

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
		return nil, fmt.Errorf("failed to fetch timeline, status code: %d", resp.StatusCode)
	}

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var timeline TimelineResponse
	if err := json.Unmarshal(respBody, &timeline); err != nil {
		fmt.Println("Error unmarshalling response:", err)
		fmt.Println("Raw response:", string(respBody))
		return nil, err
	}

	return timeline.Feed, nil
}

func replyToPost(jwt, did, postUri, postCid, text string) (string, error) {
	url := fmt.Sprintf("%s/com.atproto.repo.createRecord", blueskyAPIBase)

	payload := map[string]interface{}{
		"collection": "app.bsky.feed.post",
		"repo":       did,
		"record": map[string]interface{}{
			"$type":     "app.bsky.feed.post",
			"text":      text,
			"createdAt": time.Now().Format(time.RFC3339),
			"reply": map[string]interface{}{
				"root": map[string]string{
					"uri": postUri,
					"cid": postCid,
				},
				"parent": map[string]string{
					"uri": postUri,
					"cid": postCid,
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

	// Extract and return the URI of the created response
	if uri, ok := response["uri"].(string); ok {
		return uri, nil
	}

	return "", fmt.Errorf("response URI not found")
}

func shouldRespond(post Post, startTime time.Time) bool {
	if post.Record.CreatedAt == "" {
		fmt.Println("Skipping post with no CreatedAt field.")
		return false
	}

	postTime, err := time.Parse(time.RFC3339, post.Record.CreatedAt)
	if err != nil {
		fmt.Printf("Error parsing timestamp for post URI %s: %v\n", post.Uri, err)
		return false
	}

	// Check if the post mentions the target user
	return postTime.After(startTime) && strings.Contains(post.Record.Text, "@"+targetUserHandle)
}

func hasResponded(postUri string) bool {
	file, err := os.Open(respondedFile)
	if err != nil {
		if os.IsNotExist(err) {
			return false // File doesn't exist, no responses yet
		}
		fmt.Println("Error opening responded file:", err)
		return false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if scanner.Text() == postUri {
			return true
		}
	}

	return false
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
