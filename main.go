package main

import (
	"time"
	"fmt"
	"os"
	
	"bbb/bsky"
	"bbb/bacalhau"
	"github.com/joho/godotenv"
)

func dispatchBacalhauJobAndPostReply(session *bsky.Session, notif bsky.Notification, jobFileLink string) {
	// Log start of the function
	fmt.Println("Starting dispatchBacalhauJobAndPostReply...")
	fmt.Println("Job file link:", jobFileLink)

	// Step 1: Retrieve the job file
	jobFile, jobFileErr := bacalhau.GetLinkedJobFile(jobFileLink)
	if jobFileErr != nil {
		fmt.Println("Could not get job file to dispatch job:", jobFileErr)
		return
	}

	// Log successful retrieval of the job file
	fmt.Println("Job file retrieved successfully:", jobFile)

	// Step 2: Dispatch the job
	fmt.Println("Dispatching job to Bacalhau...")
	result := bacalhau.CreateJob(jobFile)

	fmt.Println("Got here.")

	// Check if the JobID is empty (failure case)
	if result.JobID == "" {
		replyText := "Sorry! Your Job failed to run! 😭\n\n" +
			"This could be due to:\n\n" +
			"1. Insufficient network capacity.\n" +
			"2. Missing a node with matching requirements.\n" +
			"3. Disallowed job configuration.\n" +
			"4. Unexpected error on our end. We're keeping track of potential issues!"
		sendReply(session, notif, replyText)
		return
	}

	// Step 4: Log and check for JobID in result
	fmt.Println("CreateJob result:", result)
	if result.JobID == "" {
		fmt.Println("CreateJob result does not contain a valid JobID.")
		replyText := "Your Job failed to run! 😭\n\nWe couldn't retrieve a valid JobID."
		sendReply(session, notif, replyText)
		return
	}

	// Step 5: Determine the reply text based on ExecutionID and Stdout
	var replyText string
	if result.ExecutionID != "" && result.Stdout != "" {
		// Successful execution
		replyText = fmt.Sprintf(
			"Your Bacalhau Job executed successfully 🥳🐟\n\n"+
				"Job ID: %s\nExecution ID: %s\nOutput: %s\n"+
				"🐟🐟🐟🐟🐟\n\n"+
				"Explore more with Bacalhau! Check out our docs at https://docs.bacalhau.org",
			result.JobID, result.ExecutionID, result.Stdout,
		)
		fmt.Println("Execution successful. Reply prepared:", replyText)
	} else {
		// Execution is incomplete or failed
		replyText = fmt.Sprintf(
			"Sorry! No results for your Bacalhau Job yet.\n\n"+
				"Retrieve results using the Bacalhau CLI:\n\n"+
				"1. Visit https://docs.bacalhau.org/getting-started/installation.\n"+
				"2. Run `bacalhau job describe %s` to get results.",
			result.JobID,
		)
		fmt.Println("Execution incomplete or failed. Reply prepared:", replyText)

		// Optionally stop the job after a timeout
		if result.JobID != "" {
			fmt.Println("Stopping job with JobID:", result.JobID)
			go bacalhau.StopJob(result.JobID, "The job ran too long for the Bacalhau Bot to tolerate.", true)
			// bacalhau.StopJob(result.JobID, "The job ran too long for the Bacalhau Bot to tolerate.", true)
		}
	}

	// Step 6: Send the reply
	sendReply(session, notif, replyText)
}

// Helper to send replies
func sendReply(session *bsky.Session, notif bsky.Notification, replyText string) {
	fmt.Println("Preparing to send reply...")
	responseUri, err := bsky.ReplyToMention(session.AccessJwt, notif, replyText, session.Did)
	if err != nil {
		fmt.Println("Error responding to mention:", err)
		return
	}
	fmt.Println("Reply sent successfully. Response URI:", responseUri)
	bsky.RecordResponse(responseUri)
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
			os.Exit(1)
			continue
		}

		for _, notif := range notifications {
			// Process only "mention" notifications

			isPostACommand, postComponents := bacalhau.CheckPostIsCommand(notif.Record.Text, bsky.Username)

			if notif.Reason == "mention" && bsky.ShouldRespond(notif) && !bsky.HasResponded(notif.Uri) && isPostACommand {
				fmt.Printf("Command detected: %s\n", notif.Record.Text)

				go dispatchBacalhauJobAndPostReply(session, notif, postComponents.Url)
				// dispatchBacalhauJobAndPostReply(session, notif, postComponents.Url)

				fmt.Printf("Responded to mention: %s\n", notif.Record.Text)
				bsky.RecordResponse(notif.Uri)    // Record the original mention

			}
		}

		time.Sleep(10 * time.Second)
	}
}

