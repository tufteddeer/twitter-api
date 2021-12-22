package twitter

import (
	"encoding/json"
	"fmt"
	"net/http"
)

const userFields = "user.fields=description,public_metrics,verified,profile_image_url"

type userResponse struct {
	Profile Profile `json:"data"`
}

// UserMetrics store account related metrics
type UserMetrics struct {
	Followers int `json:"followers_count"`
	Following int `json:"following_count"`
	Tweets    int `json:"tweet_count"`
}

// Profile contains a twitter users profile
type Profile struct {
	Name        string      `json:"name"`
	Handle      string      `json:"username"`
	Picture     string      `json:"profile_image_url"`
	Verified    bool        `json:"verified"`
	Description string      `json:"description"`
	Metrics     UserMetrics `json:"public_metrics"`
}

// GetProfile retrieves a users profile information
func (tw *Client) GetProfile(userID string) (profile Profile, err error) {

	uri := fmt.Sprintf("%s/users/%s?%s", apiRoot, userID, userFields)

	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return
	}

	result, err := tw.authenticatedTwitterRequest(req)
	if err != nil {
		return
	}
	defer result.Body.Close()

	var response userResponse
	err = json.NewDecoder(result.Body).Decode(&response)
	if err != nil {
		return
	}

	return response.Profile, nil
}
