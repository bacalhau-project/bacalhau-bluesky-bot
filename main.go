package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"bbb/bacalhau"
	"bbb/bsky"
	"bbb/gancho"
	"bbb/helpers"
	"bbb/s3uploader"

	"github.com/joho/godotenv"
)

func uploadResultAndGetPublicURL(key, result string) (string, error) {
	bucketName := os.Getenv("RESULTS_BUCKET")
	uploader, err := s3uploader.NewS3Uploader(bucketName)
	if err != nil {
		fmt.Printf("Failed to initialize S3Uploader: %v\n", err)
		return "", err
	}

	objectKey := key + ".txt"
	content := []byte(result)
	contentType := "text/plain"

	publicURL, err := uploader.UploadFile(objectKey, content, contentType)
	if err != nil {
		fmt.Printf("Failed to upload file: %v\n", err)
		return "", err
	}

	fmt.Printf("File uploaded successfully! Public URL: %s\n", publicURL)
	return publicURL, nil
}

func dispatchClassificationJobAndPostReply(session *bsky.Session, notif bsky.Notification, imageURL string, isHotDogJob bool, isArbitraryClassJob bool, className string) {
	// Generate and create the Bacalhau job
	bTest, bErr := bacalhau.GenerateClassificationJob(imageURL, isHotDogJob, className)
	fmt.Println("Classification job specification, error:", bTest, bErr)

	result := bacalhau.CreateJob(bTest)
	fmt.Println("Classification Job result:", result)
	fmt.Println("JobID:", result.JobID)
	fmt.Println("ExecutionID:", result.ExecutionID)
	fmt.Println("Stdout:", result.Stdout)

	// Parse out classificationID
	splitStdoutStr := ">>> Results ID <<<"
	classificationID := strings.TrimSpace(strings.Split(result.Stdout, splitStdoutStr)[1])
	fmt.Println("classificationID:", classificationID)

	// Download results (metadata + image) from S3
	objectStorageBaseURL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/", os.Getenv("S3_IMAGE_BUCKET"), os.Getenv("AWS_REGION"))

	metadataFile, metadataErr := helpers.DownloadFile(objectStorageBaseURL + classificationID + ".json")
	imageFile, imageErr := helpers.DownloadFile(objectStorageBaseURL + classificationID)

	if metadataErr != nil {
		fmt.Println("Could not retrieve result metadata:", metadataErr)
	}
	if imageErr != nil {
		fmt.Println("Could not retrieve result image:", imageErr)
	}

	// Parse JSON metadata
	var jsonObj map[string]interface{}
	err := json.Unmarshal(metadataFile, &jsonObj)
	if err != nil {
		fmt.Printf("Error parsing JSON: %v\n", err)
		return
	}

	// Extract classes from analysis text
	analysisText := strings.Split(jsonObj["resultsText"].(string), "\n")[0]
	classes := strings.Split(strings.Join(strings.Split(analysisText, " ")[3:], " "), ",")

	fmt.Println("metadataStr:", string(metadataFile))
	fmt.Println("Analysis:", analysisText)
	fmt.Println("Classes:", classes)

	// Prepare reply text
	replyText := ""
	if !isHotDogJob {
		replyText = fmt.Sprintf("Using the model '%s', ", os.Getenv("CLASSIFICATION_IMAGE"))
		if len(classes) > 0 {
			replyText += "I can see...\n\n"
			for _, class := range classes {
				replyText += fmt.Sprintf("%s\n", strings.TrimSpace(class))
			}
			replyText += "\n\n🐟🐟🐟🐟🐟🐟🐟🐟🐟🐟\n\n"
		} else {
			replyText += "I can't detect anything in that image!\n\nSorry!"
		}
	} else {
		for _, class := range classes {
			replyText += fmt.Sprintf("%s\n", strings.TrimSpace(class))
		}
	}

	// Send reply (with image)
	sendReplyWithImage(session, notif, replyText, imageFile)
}

func dispatchBacalhauJobAndPostReply(session *bsky.Session, notif bsky.Notification, jobFileLink string) {
	fmt.Println("Starting dispatchBacalhauJobAndPostReply...")
	fmt.Println("Job file link:", jobFileLink)

	// Step 1: Retrieve the job file
	jobFile, jobFileErr := bacalhau.GetJobFileFromURL(jobFileLink)
	if jobFileErr != nil {
		fmt.Println("Could not get job file to dispatch job:", jobFileErr)
		jobRetrievalErrTxt := fmt.Sprintf(
			"Sorry! Something went wrong when trying to get your Job file 😔\n\nThe error that came back was %s\n\nPlease check your Job file and try again!",
			jobFileErr,
		)
		sendReply(session, notif, jobRetrievalErrTxt)
		return
	}

	fmt.Println("Job file retrieved successfully:", jobFile)

	// Step 2: Dispatch the job
	fmt.Println("Dispatching job to Bacalhau...")
	result := bacalhau.CreateJob(jobFile)
	fmt.Println("CreateJob result:", result)

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

	// Step 5: Determine the reply text based on ExecutionID and Stdout
	var replyText string
	if result.ExecutionID != "" && result.Stdout != "" {
		jobResultContent := result.Stdout
		publicURL, uploadErr := uploadResultAndGetPublicURL(result.ExecutionID, jobResultContent)
		if uploadErr != nil {
			fmt.Println("Could not upload file to S3:", uploadErr)
			return
		}

		shortlink, slErr := gancho.GenerateShortURL(publicURL)
		if slErr != nil {
			shortlink = publicURL
		}

		replyText = fmt.Sprintf(
			"Your Bacalhau Job executed successfully 🥳🐟\n\n"+
				"Job ID: %s\nExecution ID: %s\nOutput: %s\n\n"+
				"🐟🐟🐟🐟🐟\n\n"+
				"Explore more with Bacalhau! Check out our docs at https://docs.bacalhau.org",
			result.JobID, result.ExecutionID, shortlink,
		)
		fmt.Println("Execution successful. Reply prepared:", replyText)

	} else {
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
		}
	}

	// Step 6: Send the reply
	sendReply(session, notif, replyText)
}

func sendReplyWithImage(session *bsky.Session, notif bsky.Notification, replyText string, image []byte) {
	fmt.Println("Preparing to send reply...")

	var (
		responseUri string
		err         error
	)

	if os.Getenv("DRY_RUN") != "true" {
		responseUri, err = bsky.ReplyToMentionWithImage(session.AccessJwt, notif, replyText, image, session.Did)
		if err != nil {
			fmt.Println("Error responding to mention:", err)
			return
		}
	} else {
		responseUri = "DRY_RUN_URI"
	}

	fmt.Println("Reply sent successfully. Response URI:", responseUri)
	bsky.RecordResponse(responseUri)
}

// Helper to send replies
func sendReply(session *bsky.Session, notif bsky.Notification, replyText string) {
	fmt.Println("Preparing to send reply...")

	var (
		responseUri string
		err         error
	)

	if os.Getenv("DRY_RUN") != "true" {
		responseUri, err = bsky.ReplyToMention(session.AccessJwt, notif, replyText, session.Did)
		if err != nil {
			fmt.Println("Error responding to mention:", err)
			return
		}
	} else {
		responseUri = "DRY_RUN_URI"
	}

	fmt.Println("Reply sent successfully. Response URI:", responseUri)
	bsky.RecordResponse(responseUri)
}

func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func startHTTPServer() {
	http.HandleFunc("/__gtg", healthCheckHandler)

	port := ":8080" // Default port
	if envPort := os.Getenv("PORT"); envPort != "" {
		port = ":" + envPort
	}

	fmt.Printf("Starting HTTP server on port %s\n", port)
	go func() {
		if err := http.ListenAndServe(port, nil); err != nil {
			fmt.Printf("HTTP server failed: %v\n", err)
		}
	}()
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
	fmt.Printf("Bacalhau Orchestrator Node IP: %s\n", bacalhau.BACALHAU_HOST)

	// Authenticate with Bluesky API
	session, err := bsky.Authenticate(bsky.Username, bsky.Password)
	if err != nil {
		fmt.Println("Authentication error:", err)
		return
	}

	startHTTPServer()
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
			// Process only "mention" notifications if they match a command
			isPostACommand, postComponents, commandType, className := bacalhau.CheckPostIsCommand(notif.Record.Text, bsky.Username)
			if notif.Reason == "mention" && bsky.ShouldRespond(notif) && !bsky.HasResponded(notif.Uri) && isPostACommand {
				fmt.Printf("Command detected: %s\n", notif.Record.Text)

				// Acknowledge job request
				acknowledgeJobRequest := "We got your job and we're running it now!\n\n" +
					"You should get results in a few seconds while we let it run, so hold tight and check your notifications!"
				go sendReply(session, notif, acknowledgeJobRequest)

				// Dispatch the appropriate job
				switch commandType {
					case "job_file":
						go dispatchBacalhauJobAndPostReply(session, notif, postComponents.Url)
					case "classify_image":
						go dispatchClassificationJobAndPostReply(session, notif, notif.ImageURL, false, false, className)
					case "hotdog":
						go dispatchClassificationJobAndPostReply(session, notif, notif.ImageURL, true, false, className)
					case "arbitraryClass":
						go dispatchClassificationJobAndPostReply(session, notif, notif.ImageURL, true, true, className)
				}

				fmt.Printf("Dispatched jobs and responses to mention: %s\n", notif.Record.Text)
				bsky.RecordResponse(notif.Uri) // Record the original mention
			}
		}

		time.Sleep(10 * time.Second)
	}
}
