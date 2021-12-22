package twitter

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// ShowTweetResponse represents the data sent by twitters v1.1/statuses/show.json
type ShowTweetResponse struct {
	ExtendedEntities struct {
		Media []struct {
			VideoInfo struct {
				Variants []struct {
					ContentType string `json:"content_type"`
					URL         string `json:"url"`
				} `json:"variants"`
			} `json:"video_info"`
		} `json:"media"`
	} `json:"extended_entities"`
}

// ErrEmptyMedia is returned when twitter did not return the "media" array in the response
var ErrEmptyMedia = fmt.Errorf("got empty media array")

// ErrEmptyVariants is returned when the "variants" array of twitters response is missing
var ErrEmptyVariants = fmt.Errorf("got empty variants array")

// ErrNoContentTypeMatch is returned when there is no entry with content-type video/mp4 in twitters response
var ErrNoContentTypeMatch = fmt.Errorf("could not find supported content type entry")

// GetVideoURL calls the v1.1/statuses/show endpoint and returns the first mp4 video
// url associated with a tweet
func (tw *Client) GetVideoURL(tweetID string) (string, error) {

	endpoint := "https://api.twitter.com/1.1/statuses/show.json?id=" + tweetID

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}

	response, err := tw.authenticatedTwitterRequest(req)
	if err != nil {
		return "", err
	}

	var tweetInfo ShowTweetResponse
	err = json.NewDecoder(response.Body).Decode(&tweetInfo)
	if err != nil {
		return "", err
	}

	media := tweetInfo.ExtendedEntities.Media
	if len(media) == 0 {
		return "", ErrEmptyMedia
	}
	variants := media[0].VideoInfo.Variants
	if len(variants) == 0 {
		return "", ErrEmptyVariants
	}

	for _, variant := range variants {
		if variant.ContentType == "video/mp4" {
			return variant.URL, nil
		}
	}

	return "", ErrNoContentTypeMatch
}
