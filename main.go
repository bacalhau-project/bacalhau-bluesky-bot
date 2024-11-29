package main

import (
	"time"
	"fmt"
	"os"
	
	"bbb/bsky"
	"bbb/bacalhau"
	"github.com/joho/godotenv"
)

func dispatchBacalhauJobAndPostReply(session *bsky.Session, notif bsky.Notification, jobFileLink string){

	jobFile, jobFileErr := bacalhau.GetLinkedJobFile(jobFileLink)

	fmt.Println(jobFile)

	if jobFileErr != nil {
		fmt.Println("Could not get job file to dispatch job")
		fmt.Println(jobFileErr)
		return
	}

	fmt.Println("Dispatching job to Bacalhau...")
	result, err := bacalhau.CreateJob(jobFile)

	fmt.Println(result)
	fmt.Println(err)

	replyText := ""

	if result.ExecutionID != "" && result.Stdout != ""{
		replyText = fmt.Sprintf("Your Bacalhau Job executed successfully 🥳🐟\n\nThe Job ID was: %s.\n\nThe Execution ID was : %s.\n\nThe output was: %s\n\n🐟🐟🐟🐟🐟\n\nYou can do even more with Bacalhau! Check out our docs at https://docs.bacalhau.org", result.JobID, result.ExecutionID, result.Stdout)
	} else {
		replyText = fmt.Sprintf("Sorry! We don't have results for your Bacalhau Job yet!\n\nYou can still get your results with the Bacalhau CLI. Check out https://docs.bacalhau.org/getting-started/installation - and then run...\n\nbacalhau job describe %s\n\n...to get your results!", result.JobID)

		fmt.Println(replyText)
	}

	// Respond to the mention
	responseUri, err := bsky.ReplyToMention(session.AccessJwt, notif, replyText, session.Did)
	fmt.Println("Error responding to mention:", err)

	bsky.RecordResponse(responseUri) // Record the reply

}

func main() {
	// Load environment variables
	err := godotenv.Load()
	if err != nil {
		fmt.Println("Could not find .env file. Continuing with existing environment variables.")
	}

	bsky.Username = os.Getenv("BLUESKY_USER")
	bsky.Password = os.Getenv("BLUESKY_PASS")

	if bsky.Username == "" || bsky.Password == "" {
		fmt.Println("Missing environment variables. Please set BLUESKY_USER and BLUESKY_PASS.")
		os.Exit(1)
	}

	bacalhau.BACALHAU_HOST = os.Getenv("BACALHAU_HOST")
	fmt.Println(bacalhau.BACALHAU_HOST)

	// Authenticate with Bluesky API
	session, err := bsky.Authenticate(bsky.Username, bsky.Password)
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

			isPostACommand, postComponents := bacalhau.CheckPostIsCommand(notif.Record.Text, bsky.Username)

			if notif.Reason == "mention" && bsky.ShouldRespond(notif) && !bsky.HasResponded(notif.Uri) && isPostACommand {
				fmt.Printf("Command detected: %s\n", notif.Record.Text)

				go dispatchBacalhauJobAndPostReply(session, notif, postComponents.Url)

				fmt.Printf("Responded to mention: %s\n", notif.Record.Text)
				bsky.RecordResponse(notif.Uri)    // Record the original mention

			}
		}

		time.Sleep(10 * time.Second)
	}
}

