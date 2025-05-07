package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
)

func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {
	// make the s3 client
	presignClient := s3.NewPresignClient(s3Client)

	// Set the expire time
	preSign, err := presignClient.PresignGetObject(context.Background(), &s3.GetObjectInput{
		Bucket: &bucket,
		Key: &key,
	}, s3.WithPresignExpires(expireTime))
	if err != nil {
		return "", fmt.Errorf("error generating presigned URL: %s", err)
	}

	return preSign.URL, nil
}

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	// check if videoURL is nil
	if video.VideoURL == nil {
		return video, nil
	}

	// split the url in bucket and key
	videoSplit := strings.Split(*video.VideoURL, ",")
	if len(videoSplit) != 2 {
		return video, fmt.Errorf("invalid video URL format: %s", *video.VideoURL)
	}

	// store them for ease of use
	bucket := videoSplit[0]
	key := videoSplit[1]

	// set the expire time 
	expireTime := 15 * time.Minute

	// get the url
	preSignedUrl, err := generatePresignedURL(cfg.s3Client, bucket, key, expireTime)
	if err != nil {
		return video, err
	}

	// set the url to the presigned url
	video.VideoURL = &preSignedUrl

	return video, nil
}

