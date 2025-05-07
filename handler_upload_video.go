package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"os"
	"os/exec"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	// set the max size (1 << 30 is 1 gb)
	const uploadLimit = 1 << 30
	r.Body = http.MaxBytesReader(w, r.Body, uploadLimit)

	// get the videoID
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	// get the token
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	// authenticate the user and get the userID
	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	// get the video
	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't find video", err)
		return
	}

	// check if user owns the video
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Not authorized to update this video", nil)
		return
	}

	// get the uploaded file in memory
	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	// get the uploaded file type and validate it
	mediaType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid Content-Type", err)
		return
	}
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Invalid file type, only MP4 is allowed", err)
		return
	}

	// upload to a temp location on disk
	tmpFile, err := os.CreateTemp("", "Tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not create temp file", err)
		return
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// write the actual file
	if _, err = io.Copy(tmpFile, file); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not write file to disk", err)
		return
	}

	// reset tmpFile pointer to the beginning
	_, err = tmpFile.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not reset file pointer", err)
		return
	}

	// get the aspect ration
	ratio, err := getVideoAspectRatio(tmpFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't get ratio", err)
		return
	}

	// get the correct prefix
	var prefix string
	switch ratio {
	case "16:9":
    	prefix = "landscape"
	case "9:16":
    	prefix = "portrait"
	default:
    	prefix = "other"
	}

	// get the bucket key
	key := prefix + "/" + getAssetPath(mediaType)

	// upload the video to s3
	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket: &cfg.s3Bucket,
		Key: &key,
		Body: tmpFile,
		ContentType: aws.String(mediaType),
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error uploading file to S3", err)
		return
	}

	// set the video url and update the video in memory
	videoURL := cfg.getObjectURL(key)
	video.VideoURL = &videoURL

	// udate the video URL in database
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}

func getVideoAspectRatio(filePath string) (string, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	var outputBuffer bytes.Buffer
	cmd.Stdout = &outputBuffer

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ffprobe error: %v", err)
	}

	type FFProbeResponse struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}

	var response FFProbeResponse
    if err := json.Unmarshal(outputBuffer.Bytes(), &response); err != nil {
        return "", fmt.Errorf("couldn't unmarshal JSON: %v", err)
    }

	if len(response.Streams) == 0 {
        return "", errors.New("no video streams found")
    }

	ratioCalc := float64(response.Streams[0].Width) / float64(response.Streams[0].Height) 
	ratioString := ""
	if math.Abs(ratioCalc - 1.78) < 0.05 {
		ratioString = "16:9"
	} else if math.Abs(ratioCalc - 0.5625) < 0.05 {
		ratioString = "9:16"
	} else {
		ratioString = "other"
	}
	return ratioString, nil
}