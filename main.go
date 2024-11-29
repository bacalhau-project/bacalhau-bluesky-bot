package main

import (
	"time"
	"fmt"
	"os"
	
	"bbb/bsky"
	"github.com/joho/godotenv"
)

var (
	blueskyAPIBase = "https://bsky.social/xrpc"
	username       = ""
	password       = ""
)


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
	session, err := bsky.Authenticate(username, password)
	if err != nil {
		fmt.Println("Authentication error:", err)
		return
	}

	bsky.StartTime = time.Now()

	// Poll notifications every 10 seconds
	for {
		fmt.Println("Fetching notifications...")
		notifications, err := bsky.FetchNotifications(session.AccessJwt)
		if err != nil {
			fmt.Println("Error fetching notifications:", err)
			time.Sleep(10 * time.Second)
			continue
		}

		for _, notif := range notifications {
			// Process only "mention" notifications
			if notif.Reason == "mention" && bsky.ShouldRespond(notif) && !bsky.HasResponded(notif.Uri) {
				fmt.Printf("Mention detected: %s\n", notif.Record.Text)

				// Respond to the mention
				replyText := "Ping!"
				responseUri, err := bsky.ReplyToMention(session.AccessJwt, notif, replyText, session.Did)
				if err != nil {
					fmt.Println("Error responding to mention:", err)
				} else {
					fmt.Printf("Responded to mention: %s\n", notif.Record.Text)
					bsky.RecordResponse(notif.Uri)    // Record the original mention
					bsky.RecordResponse(responseUri) // Record the reply
				}
			}
		}

		time.Sleep(10 * time.Second)
	}
}

