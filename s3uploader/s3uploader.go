package s3uploader

import (
	"bytes"
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	// "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type S3Uploader struct {
	client *s3.Client
	bucket string
}

// NewS3Uploader initializes an S3Uploader instance.
// `bucketName` specifies the S3 bucket to which files will be uploaded.
func NewS3Uploader(bucketName string) (*S3Uploader, error) {
	// Load AWS credentials from environment variables or IAM role
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(os.Getenv("AWS_REGION")))
	if err != nil {
		return nil, fmt.Errorf("unable to load AWS SDK config: %v", err)
	}

	return &S3Uploader{
		client: s3.NewFromConfig(cfg),
		bucket: bucketName,
	}, nil
}

// UploadFile uploads the provided content to S3.
// `objectKey` is the key (filename) in the bucket, and `content` is the data to upload.
func (u *S3Uploader) UploadFile(objectKey string, content []byte, contentType string) (string, error) {
	// Define input for the PutObject operation
	input := &s3.PutObjectInput{
		Bucket:      aws.String(u.bucket),
		Key:         aws.String(objectKey),
		Body:        bytes.NewReader(content),
		ContentType: aws.String(contentType),
		// ACL:         types.ObjectCannedACLPublicRead, // Makes the object publicly accessible
	}

	// Upload the object
	_, err := u.client.PutObject(context.TODO(), input)
	if err != nil {
		return "", fmt.Errorf("failed to upload file to S3: %v", err)
	}

	// Return the public URL for the uploaded object
	url := fmt.Sprintf("https://%s.s3.amazonaws.com/%s", u.bucket, objectKey)
	return url, nil
}
