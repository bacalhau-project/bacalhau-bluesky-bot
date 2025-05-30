package main

import (
	"fmt"
	"os"
	"strings"
	"time"
	"strconv"
	"encoding/json"
	"math/rand"

	"bbb/bacalhau"
	"bbb/bsky"
	"bbb/gancho"
	"bbb/helpers"
	"bbb/s3uploader"

	"github.com/joho/godotenv"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/template/handlebars/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/google/uuid"
)

var DEFAULT_JOB_WAIT_TIME int
var UUIDRouteRegex string = "<regex(^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-4[a-fA-F0-9]{3}-[8|9|aA|bB][a-fA-F0-9]{3}-[a-fA-F0-9]{12}$)}>"
var EXPANSO_BOTS []string
var COMMUNITY_BOTS []CommunityBot

type CommunityBot struct {
	Name string `json:"name"`
	Storage bool `json:"storage"`
	EnvironmentVariables []string `json:"environmentVariables"`
	JobFile string `json:jobFile`
}

func isExpansoBotAccount(botHandle string) (bool) {

	var isExpansoBot bool = false

	for _, expansoBotHandle := range EXPANSO_BOTS {

		if botHandle == expansoBotHandle {
			isExpansoBot = true
			break
		}

	}

	fmt.Printf("Is %s an Expanso bot? %t\n", botHandle, isExpansoBot)

	return isExpansoBot

}

func loadCommunityBotDetails(ref *[]CommunityBot) error {

	candidateBots, err := os.ReadDir("./community")

	if err != nil {
        return err
    }
 
    for _, cB := range candidateBots {
		// fmt.Println(e.Name())
		// *ref = append(*ref, e.Name()) // Correct: Assign back to *ref

		workingDir := fmt.Sprintf("./community/%s/", cB.Name())

		infoFile, iErr := os.ReadFile(fmt.Sprintf("%s/info.json", workingDir))
		jobFile, jErr := os.ReadFile(fmt.Sprintf("%s/job.yaml", workingDir))

		if iErr != nil {
			fmt.Println("Error loading %s info file:", iErr.Error())
			continue
		}

		if jErr != nil {
			fmt.Println("Error loading %s job file:", jErr.Error())
			continue
		}

		bot := CommunityBot{}

		unmarshalErr := json.Unmarshal(infoFile, &bot)
		if unmarshalErr != nil {
			fmt.Printf("Error parsing Community Bot JSON: %s\n", unmarshalErr.Error())
			continue
		}

		bot.JobFile = string(jobFile)

		*ref = append(*ref, bot)

    }

	return nil

}

func startCommunityJob(session *bsky.Session, notif bsky.Notification, bot CommunityBot, accountName string) {

	processedPost := strings.Replace(notif.Record.Text, fmt.Sprintf("@%s", accountName), "", -1)

	envVarValues := map[string]interface{}{
		"POST" : notif.Record.Text,
		"FROM" : notif.Author.Handle,
		"IMAGES" : notif.Post.Images,
		"PROCESSED_POST" : processedPost,
		"WHOAMI" : accountName,
	}
	
	envLoadErr := bacalhau.LoadEnvVarsToJob(&bot.JobFile, bot.EnvironmentVariables, envVarValues)
	if envLoadErr != nil {
		fmt.Println("Could not load env vars to Community Bot Job file.", envLoadErr.Error())
		return
	}
	
	jobJSON, convErr := bacalhau.ConvertYamlToJSON(bot.JobFile)

	if convErr != nil {
		fmt.Println("Failed to convert Job file to JSON:", convErr.Error())
		return
	}

	fmt.Println("jobJSON:", jobJSON)

	jobJSON["Name"] = fmt.Sprintf("%s (community)", jobJSON["Name"])

	wrappedJob := map[string]interface{}{
		"Job": jobJSON,
	}

	marshalledJSON, mErr := json.Marshal(wrappedJob)

	if mErr != nil {
		fmt.Println("Could not marshall JSON for Community Bot Job.", mErr.Error())
	}

	communityBotResult := bacalhau.CreateJob(string(marshalledJSON), 5)
	fmt.Printf(`Community bot "%s" result: %s`, bot.Name, communityBotResult)
	fmt.Println("JobID:", communityBotResult.JobID)
	fmt.Println("ExecutionID:", communityBotResult.ExecutionID)
	fmt.Println("Stdout:", communityBotResult.Stdout)

	sendReply(session, notif, communityBotResult.Stdout)

}

func generateFailureResponse() string { 

	var possibleErrorResponses = []string{
		"Sorry! Something went wrong with the bot! Please try again later",
		"Oh no! Something didn't work right with this bot! Sorry!",
		"This bot experienced an error trying to respond to you - apologies! Please try again in a bit.",
		"Something didn't quite work as expected. Sorry! Please try again in a little while.",
	}

	selectedErrorResponse := possibleErrorResponses[rand.Intn(len(possibleErrorResponses))]

	fmt.Println("selectedErrorResponse:", selectedErrorResponse)

	return selectedErrorResponse

}

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

	result := bacalhau.CreateJob(bTest, DEFAULT_JOB_WAIT_TIME)
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

//>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>
//>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>
// ALT TEXT JOB
//<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<
//<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<

func respondWhileAltTextIsDown(session *bsky.Session, notif bsky.Notification) {

	var possibleResponses = []string{
		"The alt-text bot is offline for maintenance at the moment, so we're not able to get you alt-text at this time. Sorry!",
		"Sorry, the alt-text bot is offline while we undertake some maintenance at the moment. Please try again later.",
		"The alt-text bot is undergoing maintenance at the moment, so we're not able to get you some alt-text at this time. Please try again later.",
	}

	selectedResponse := possibleResponses[ rand.Intn( len( possibleResponses ) ) ]

	sendReply(session, notif, selectedResponse)

}

func dispatchAltTextJobAndPostReply(session *bsky.Session, notif bsky.Notification) {

	// 1. Image in post
	// 2. Image in quoted post
	// 3. Image in parent post
	// 4. None.

	resultsUUID := uuid.New().String()

	var possibleResponsesForMissingImages = []string{
		"It doesn't look like there were any images that we could generate alt-text for in that post. Sorry!",
		"Couldn't find any images to generate alt-text for. Sorry!",
		"Hmmm... the bot couldn't pick out any image to generate alt-text for in your request. Please try again. Sorry!",
	}

	selectedEmptyResponse := possibleResponsesForMissingImages[rand.Intn(len(possibleResponsesForMissingImages))]

	var imageToGenerateAltTextFor string

	if notif.Post.PostType == "reply" {

		parentPost, pPostErr := bsky.GetRepliedToPost(session.AccessJwt, notif)

		if pPostErr != nil {
			fmt.Printf("Parent Post err: %s\n", pPostErr.Error())
		}

		fmt.Printf("Parent Post: %+v\n", parentPost)

		if len(parentPost.Images) > 0 {
			imageToGenerateAltTextFor = parentPost.Images[0].Url
		} else {
			// Handle no images being present
			sendReply(session, notif, selectedEmptyResponse)
			return
		}

	}

	if notif.Post.PostType == "quote" {

		fmt.Println("Nested Post:", notif.Record.Embed.Record.Uri)

		nestedPost, nPErr := bsky.GetPostByUri(session.AccessJwt, notif.Record.Embed.Record.Uri)

		if nPErr != nil {
			fmt.Println("nPErr:", nPErr.Error())
			failureResponse := generateFailureResponse()
			sendReply(session, notif, failureResponse)
			return
		} else {

			if len(nestedPost.Images) > 0 {
				imageToGenerateAltTextFor = nestedPost.Images[0].Url
			} else {
				// Handle no images being present
				sendReply(session, notif, selectedEmptyResponse)
				return
			}

		}

	}

	if notif.Post.PostType == "post" {

		if len(notif.Post.Images) > 0 {
			fmt.Println("Image in post:", notif.Post.Images[0].Url)
			imageToGenerateAltTextFor = notif.Post.Images[0].Url
		} else {
			// Handle no images being present
			sendReply(session, notif, selectedEmptyResponse)
			return
		}

	}

	fmt.Println("Determined type:", notif.Post.PostType)
	fmt.Println("Selected Image:", imageToGenerateAltTextFor)

	prompt := os.Getenv("ALT_TEXT_JOB_PROMPT")

	if prompt == "" {
		prompt = "Briefly, what is in this image?"
	}

	fmt.Println("Prompt Text:", prompt)

	altTextJob, jErr := bacalhau.GenerateAltTextJob(imageToGenerateAltTextFor, prompt)

	if jErr != nil {
		fmt.Printf("Could not generate alt-text Job file: %s", jErr.Error())
		failureResponse := generateFailureResponse()
		sendReply(session, notif, failureResponse)
		return
	}

	altTextResult := bacalhau.CreateJob(altTextJob, 20)
	fmt.Println("Alt-text result:", altTextResult)
	fmt.Println("JobID:", altTextResult.JobID)
	fmt.Println("ExecutionID:", altTextResult.ExecutionID)
	fmt.Println("Stdout:", altTextResult.Stdout)

	ocrJob, ocrJErr := bacalhau.GenerateOCRJob(imageToGenerateAltTextFor)

	if ocrJErr != nil {
		fmt.Printf("Could not generate alt-text Job file: %s", ocrJErr.Error())
	}

	ocrTextResult := bacalhau.CreateJob(ocrJob, 10)
	fmt.Println("OCR result:", ocrTextResult)
	fmt.Println("JobID:", ocrTextResult.JobID)
	fmt.Println("ExecutionID:", ocrTextResult.ExecutionID)
	fmt.Println("Stdout:", ocrTextResult.Stdout)

	if altTextResult.Stdout == ""{

		fmt.Printf(`Job "%s" failed to produce alt-text in the permitted timeframe.`, altTextResult.JobID)

		errorResponseTxt := generateFailureResponse()
		sendReply(session, notif, errorResponseTxt)

	} else {

		var truncatedAltText string

		splitAltText := strings.Split(altTextResult.Stdout, ". ")

		for _, sentence := range splitAltText {

			if len(truncatedAltText) + (len(sentence) + 1) < 220 {
				truncatedAltText += fmt.Sprintf("%s. ", sentence)
			} else {
				continue
			}

		}

		fmt.Println("truncatedAltText:", truncatedAltText)

		payload := map[string]string{
			"ALT_TEXT" : altTextResult.Stdout,
			"OCR_TEXT" : ocrTextResult.Stdout,
			"IMAGE_URL" : imageToGenerateAltTextFor,
		}

		payloadBytes, payloadMarshallErr := json.Marshal(payload)
		if payloadMarshallErr != nil {

			fmt.Printf("Unable to marshall result for upload: %s", payloadMarshallErr.Error())
			sendReply(session, notif, truncatedAltText)

			return

		} else {

			_, uploadErr := uploadResultAndGetPublicURL(resultsUUID, string(payloadBytes))
	
			if uploadErr != nil {
				fmt.Println("Failed to upload result to Object Storage:", uploadErr.Error())
				sendReply(session, notif, truncatedAltText)
			} else {
	
				// Generate shortURL and payload in 
				// preparation for results display

				targetURL := fmt.Sprintf("%s/alt-text-result/%s", os.Getenv("SERVER_ORIGIN"), resultsUUID)

				shortURL, sURLErr := gancho.GenerateShortURL(targetURL)

				if sURLErr != nil {
					fmt.Println("Could not generate shortURL for results with Gancho:", sURLErr)
					sendReply(session, notif, truncatedAltText)
				} else {

					fmt.Println("shortURL:", shortURL)

					shortLinkStr := "\n\nLonger description + OCR:\n" + shortURL

					truncatedAltText += shortLinkStr
					sendReply(session, notif, truncatedAltText)

				}
	
			}

		}

	}

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
	result := bacalhau.CreateJob(jobFile, DEFAULT_JOB_WAIT_TIME)
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

func startHTTPServer() {
	// http.HandleFunc("/__gtg", healthCheckHandler)

	port := ":8080" // Default port
	if envPort := os.Getenv("PORT"); envPort != "" {
		port = ":" + envPort
	}

	engine := handlebars.New("./views", ".hbs")

	app := fiber.New(fiber.Config{
		DisableStartupMessage : true,
		Views: engine,
		ServerHeader: "bacalhau-bluesky-bot",
		AppName: "bacalhau-bluesky-bot",
	})

	app.Use(compress.New(compress.Config{
		Level: compress.LevelBestSpeed,
	}))

	app.Static("/", "./static")

    // Define a route for the GET method on the root path '/'
    app.Get("/__gtg", func(c *fiber.Ctx) error {
        // Send a string response to the client
		c.Status(200)
        return nil
    })

	app.Get("/alt-text-result/:uuid" + UUIDRouteRegex, func(c *fiber.Ctx) error {
		
		fmt.Println("Alt-Text Results ID:", c.Params("uuid"))

		resultsKey := fmt.Sprintf("%s.txt", c.Params("uuid"))
		
		bucketName := os.Getenv("RESULTS_BUCKET")
		s3Client, clientErr := s3uploader.NewS3Uploader(bucketName)

		if clientErr != nil {
			fmt.Println("Client err:", clientErr.Error())
		}

		results, rErr := s3Client.GetObject(resultsKey)

		if rErr != nil {
			fmt.Println("S3 Err:", rErr.Error())
			return rErr
		} else {

			var jsonObj map[string]interface{}
			unmarshalErr := json.Unmarshal(results, &jsonObj)
			if unmarshalErr != nil {
				fmt.Printf("Error parsing JSON: %v\n", unmarshalErr)
				return unmarshalErr
			}

			fmt.Println(jsonObj["ALT_TEXT"].(string))

			altText, altTextOk := jsonObj["ALT_TEXT"].(string)
			ocrText, ocrTextOk := jsonObj["OCR_TEXT"].(string)
			imageURL, imageURLOk := jsonObj["IMAGE_URL"].(string)

			// Handle missing or invalid values
			if !altTextOk {
				altText = "" // Default to an empty string or handle it as needed
			}
			if !ocrTextOk {
				ocrText = ""
			}
			if !imageURLOk {
				imageURL = ""
			}

			fmt.Println("OCR_TEXT:", ocrText)

			content := fiber.Map{
				"LVM_TEXT" : altText,
				"IMAGE_URL" : imageURL,
			}
			
			if ocrText != "" {

				formattedOCR := strings.ReplaceAll(ocrText, "\n", "<br/>")

				content = fiber.Map{
					"LVM_TEXT" : altText,
					"OCR_TEXT" : formattedOCR,
					"IMAGE_URL" : imageURL,
				}
			}
			
			fmt.Printf("%+v", content)

			return c.Render("alt-text", content, "layouts/main")

		}

	})

    // Start the server on port 3000
    err := app.Listen(port)

	if err != nil {
		fmt.Printf("HTTP server failed: %s\n", err.Error())
		fmt.Println("Unexpected condition in application. Exiting.")
		os.Exit(1)
	}

}

func main() {
	// Load environment variables
	err := godotenv.Load()
	if err != nil {
		fmt.Println("Could not find .env file. Continuing with existing environment variables.")
	}

	var BLUESKY_ACCOUNTS []map[string]interface{}

	accountLoadErr := json.Unmarshal([]byte(os.Getenv("BLUESKY_ACCOUNTS")), &BLUESKY_ACCOUNTS)

	if accountLoadErr != nil {
		fmt.Println("Could not load bot account credentials from environment variables. Exiting.")
		fmt.Printf("Error: %s\n", accountLoadErr.Error())
		os.Exit(1)
	}

	if len(BLUESKY_ACCOUNTS) == 0 {
		fmt.Println("No account credentials have been set with the BLUESKY_ACCOUNTS environment variable. A stringified JSON object with at least 1 username:password combo should be set for program to execute. Exiting.")
		os.Exit(1)
	}

	bacalhau.BACALHAU_HOST = os.Getenv("BACALHAU_HOST")
	fmt.Printf("Bacalhau Orchestrator Hostname: %s\n", bacalhau.BACALHAU_HOST)

	if os.Getenv("DEFAULT_JOB_WAIT_TIME") == "" {
		DEFAULT_JOB_WAIT_TIME = 30
	} else {
		
		waitTime, convErr := strconv.Atoi( os.Getenv("DEFAULT_JOB_WAIT_TIME") )

		if convErr != nil {
			fmt.Println(fmt.Sprintf(`An error occured converting DEFAULT_JOB_WAIT_TIME environment variable to an integer. Defaulting to 30 seconds: %s`, convErr.Error()))
			DEFAULT_JOB_WAIT_TIME = 30
		} else {
			DEFAULT_JOB_WAIT_TIME = waitTime
		}

	}

	EXPANSO_BOTS = strings.Split( os.Getenv("EXPANSO_BOTS"), ",")
	fmt.Println("EXPANSO_BOTS:", EXPANSO_BOTS)

	loadCommunityBotDetails(&COMMUNITY_BOTS)

	// Start HTTP server for healthchecks
	go startHTTPServer()

	bsky.StartTime = time.Now()

	for {

		for idx, bskyAccount := range BLUESKY_ACCOUNTS {

			bskyHandle, handleOk := bskyAccount["username"].(string)
			bskyPass, passOk := bskyAccount["pass"].(string)

			if !handleOk || !passOk {
				fmt.Printf("Invalid account data at index %d\n", idx)
				continue
			}

			fmt.Printf("Attempting to authenticate for user: %s\n", bskyHandle)

			go func(username, password string) {

				// Authenticate with Bluesky API
				session, err := bsky.Authenticate(username, password)
				if err != nil {
					fmt.Println( fmt.Sprintf(`Could not authenticate "%s": %s`, username, err.Error()) )
					return
				}

				fmt.Println( fmt.Sprintf(`Fetching notifications for handle "%s"...`, bskyHandle) )

				notifications, err := bsky.FetchNotifications(session.AccessJwt)
				if err != nil {
					fmt.Printf("Error fetching notifications for handle %s: %s\n", bskyHandle, err.Error())
					time.Sleep(10 * time.Second)
					return
				}

				if isExpansoBotAccount(bskyHandle) {
	
					for _, notif := range notifications {
					// Process only "mention" notifications if they match a command

						isPostACommand, postComponents, commandType, className := bacalhau.CheckPostIsCommand(notif.Record.Text, username)
						
						if notif.Reason == "mention" && bsky.ShouldRespond(notif) && !bsky.HasResponded(notif.Uri) && isPostACommand {
							

								fmt.Printf("Command detected: %s\n", notif.Record.Text)
		
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
									case "altText":
										go dispatchAltTextJobAndPostReply(session, notif)
								}
		
								fmt.Printf("Dispatched jobs and responses to mention: %s\n", notif.Record.Text)
								bsky.RecordResponse(notif.Uri)

								
						}

						
					}

				} else {

					// Start working through the community bots...

					for _, communityBot := range COMMUNITY_BOTS{

						fmt.Println("Community Bot name:", communityBot.Name)

						for _, notif := range notifications {

							// targetHandle := notif.Author.Handle
							// fmt.Println(targetHandle)

							// targetAccount := strings.Split(username, ".bots.bacalhau.org")[0]

							// fmt.Println("targetAccount:", targetAccount)
							// fmt.Println("targetAccount == Bot?", targetAccount == communityBot.Name)

							if notif.Reason == "mention" && bsky.ShouldRespond(notif) && !bsky.HasResponded(notif.Uri){

								// fmt.Println("Would respond to this notification:", notif.Uri)

								go startCommunityJob(session, notif, communityBot, username)

								bsky.RecordResponse(notif.Uri)

							}

						}

					}

				}
				

			}(bskyHandle, bskyPass)

		}

		time.Sleep(10 * time.Second)
		fmt.Println("Waiting 10 seconds...")

	}


}
