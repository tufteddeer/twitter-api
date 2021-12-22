package twitter

import (
	"bytes"
	"log"
	"testing"
)

func TestTweetsFromSearchResults(t *testing.T) {

	users := []user{
		{
			ID:     "1",
			Name:   "one",
			Handle: "@one"},
		{
			ID:     "2",
			Name:   "two",
			Handle: "@two"},
	}

	media := []media{
		{MediaKey: "mymediakey", Type: "photo", URL: "example.org"},
	}

	attachments := attachments{MediaKeys: []string{"mymediakey"}}

	tweets := []tweet{
		{
			AuthorID:    "1",
			Text:        "text from one",
			ID:          "t1",
			CreatedAt:   "123",
			Metrics:     metrics{Likes: 5},
			Attachments: attachments,
		},
		{
			AuthorID:  "2",
			Text:      "text from two",
			ID:        "t2",
			CreatedAt: "456",
			Metrics:   metrics{Likes: 15},
		},
	}
	searchResponse := searchResponse{
		Tweets: tweets,
	}

	searchResponse.Includes.Users = users
	searchResponse.Includes.Media = media

	convertedTweets := tweetsFromSearchResult(searchResponse)

	for i, tweet := range convertedTweets {
		expected := tweets[i]
		equals(tweet.Text, expected.Text)
		equals(tweet.Likes, expected.Metrics.Likes)
		equals(tweet.Author.Handle, users[i].Handle)
		equals(tweet.Created, expected.CreatedAt)
		equals(tweet.Likes, expected.Metrics.Likes)
	}

	equals(convertedTweets[0].Images[0], "example.org")
	equals(len(convertedTweets[1].Images), 0)
}

func TestConvertToTweet(t *testing.T) {

	tweeet := tweet{
		Text:     "abc",
		AuthorID: "1",
		ID:       "tweetid",
	}
	u := user{
		ID:       "1",
		Name:     "one",
		Handle:   "@one",
		Verified: true,
	}

	incl := includes{
		Users: []user{u},
		Media: nil,
	}
	niceTweet := convertToTweet(tweeet, incl, nil)

	equals(niceTweet.Text, "abc")
	equals(niceTweet.Author.Handle, "@one")
	equals(niceTweet.ID, "tweetid")
	equals(niceTweet.HasVideo, false)
	matches := []streamRuleInt{{ID: 123}}

	tweetWithRule := convertToTweet(tweeet, incl, &matches)

	equals(niceTweet.Text, "abc")
	equals(niceTweet.Author.Handle, "@one")
	equals(len(tweetWithRule.RuleIDs), 1)
	equals(tweetWithRule.RuleIDs[0], "123")

	// convert tweet with video
	tweeet = tweet{
		Text:        "abc",
		AuthorID:    "1",
		ID:          "tweetid",
		Attachments: attachments{MediaKeys: []string{"videomediakey"}},
	}
	incl = includes{
		Users: []user{u},
		Media: []media{
			{
				MediaKey:             "videomediakey",
				Type:                 "video",
				URL:                  "https://example.org/video.mp4",
				VideoPreviewImageURL: "https://example.org/preview",
			},
		},
	}
	niceTweet = convertToTweet(tweeet, incl, nil)

	equals(niceTweet.Text, "abc")
	equals(niceTweet.Author.Handle, "@one")
	equals(niceTweet.ID, "tweetid")
	equals(niceTweet.HasVideo, true)
	equals(niceTweet.VideoPreviewURL, "https://example.org/preview")
	equals(niceTweet.Author.Verified, true)
}

// equals checks if the supplied values are equal.
// If they are not, both values are logged and the program exits
func equals(actual, expected interface{}) {
	switch actual.(type) {
	case []byte:
		if bytes.Equal(actual.([]byte), expected.([]byte)) == false {
			log.Fatalf("Expected %v, got %v", expected, actual)
		}
	default:
		if actual != expected {
			log.Fatalf("Expected %v, got %v", expected, actual)
		}
	}

}
