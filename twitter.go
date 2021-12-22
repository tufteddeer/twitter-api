/*Package twitter contains structs representing Twitter API objects and functions to
 interact with the API.

The package supports the v2 streaming endpoint. To stream tweets, at least one StreamRule must be added
using CreateStreamRule.
The rule can then be passed to SubscribeStream to get a StreamSubscription, which contains a channel Tweets
that receives incoming Tweets matching the rule.

Streaming is started using StartStream and stops when every subscription is removed using UnsubscribeStream or after
StopStream is called.
*/
package twitter

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
)

const apiRoot = "https://api.twitter.com/2"
const expansionsAndFields = "expansions=author_id,attachments.media_keys,attachments.poll_ids" +
	"&tweet.fields=author_id,created_at,text,public_metrics,possibly_sensitive" +
	"&user.fields=profile_image_url,verified" +
	"&media.fields=type,url,media_key,preview_image_url"

// Author represents a Twitter user who wrote a tweet
type Author struct {
	Name     string `json:"name"`
	Handle   string `json:"handle"`
	Picture  string `json:"picture"`
	Verified bool   `json:"verified"`
	ID       string `json:"id"`
}

// Tweet represents a tweet with all relevant metadata
type Tweet struct {
	ID              string   `json:"id"`
	Text            string   `json:"text"`
	Author          Author   `json:"author"`
	Created         string   `json:"created"`
	Images          []string `json:"images"`
	Retweets        int      `json:"retweets"`
	Replies         int      `json:"replies"`
	Likes           int      `json:"likes"`
	Quotes          int      `json:"quotes"`
	RuleIDs         []string
	HasVideo        bool         `json:"hasVideo"`
	VideoPreviewURL string       `json:"videoPreviewURL"`
	Sensitive       bool         `json:"sensitive"`
	Poll            []PollOption `json:"poll,omitempty"`
}

// Client provides access to (some) twitter api endpoints
type Client struct {
	Token                  string
	streamSubscribers      []StreamSubscription
	streaming              bool
	stopStreamChan         chan bool
	StreamedTweets         chan Tweet // every Tweet received from the streaming endpoint, regardless of matching rules
	EnableAllTweetsChannel bool
	logger                 *log.Logger
	sync.Mutex
}

// tweet represents how the twitter api describes tweets
type tweet struct {
	AuthorID    string `json:"author_id"`
	Text        string
	ID          string
	CreatedAt   string      `json:"created_at"`
	Metrics     metrics     `json:"public_metrics"`
	Attachments attachments `json:"attachments"`
	Sensitive   bool        `json:"possibly_sensitive"`
}

// user is a twitter user as given by the api
type user struct {
	ID       string `json:"id"`
	Name     string
	Handle   string `json:"username"`
	Picture  string `json:"profile_image_url"`
	Verified bool
}

// metrics describes user interacting with a tweet
type metrics struct {
	Retweets int `json:"retweet_count"`
	Replies  int `json:"reply_count"`
	Likes    int `json:"like_count"`
	Quotes   int `json:"quote_count"`
}

// attachments is sued to store media keys
type attachments struct {
	MediaKeys []string `json:"media_keys"`
	PollIDs   []string `json:"poll_ids"`
}

// media is used to resolve media_keys from attachments to urls
type media struct {
	MediaKey             string `json:"media_key"`
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	VideoPreviewImageURL string `json:"preview_image_url"`
}

// includes holds metadata for tweets like media and user(s)
type includes struct {
	Users []user
	Media []media
	Polls []struct {
		Options []PollOption `json:"options"`
		ID      string       `json:"id"`
	} `json:"polls"`
}

// searchResponse represents the data returned by twitters search api
type searchResponse struct {
	Tweets   []tweet  `json:"data"`
	Includes includes `json:"includes"`
}

// PollOption represents a possible answer in a Poll
type PollOption struct {
	Position int    `json:"position"`
	Label    string `json:"label"`
	Votes    int    `json:"votes"`
}

// New creates a new Client with the given token
func New(token string) Client {
	return Client{
		Token:          token,
		logger:         log.New(os.Stdout, "[twitter] ", log.Ldate|log.Ltime|log.Lmsgprefix|log.Lshortfile),
		stopStreamChan: make(chan bool),
		StreamedTweets: make(chan Tweet),
	}
}

const (
	// ImageFilter searches only for tweets that have an image attached if added to a stream rule
	ImageFilter = "has:images"
	// ExcludeRetweetsFilter excludes retweets if added to a stream rule
	ExcludeRetweetsFilter = "-is:retweet"
)

// authenticatedTwitterRequest adds an authentication token to the request header,
// sends the request and returns the response
func (tw *Client) authenticatedTwitterRequest(request *http.Request) (response *http.Response, err error) {
	request.Header.Set("Authorization", "Bearer "+tw.Token)

	client := http.Client{}

	httpResponse, err := client.Do(request)
	if err != nil {
		return
	}

	return httpResponse, nil
}

// SearchRecent sends a query to the /2/tweets/search/recent Twitter API
// and returns the received tweets.
// Options accepts several strings for keywords or options like ImageFilter
func (tw *Client) SearchRecent(options ...string) (tweets []Tweet, err error) {

	queryBuilder := strings.Builder{}

	for _, option := range options {
		queryBuilder.WriteString(option)
		queryBuilder.WriteString(" ")
	}
	escapedQuery := url.QueryEscape(queryBuilder.String()) // https://stackoverflow.com/questions/58419348/is-there-a-urlencode-function-in-golang
	uri := fmt.Sprintf("%s/tweets/search/recent?query=%s&max_results=10&%s", apiRoot, escapedQuery, expansionsAndFields)

	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return
	}

	result, err := tw.authenticatedTwitterRequest(req)
	if err != nil {
		return
	}
	defer result.Body.Close()

	var searchResponse searchResponse
	err = json.NewDecoder(result.Body).Decode(&searchResponse)
	if err != nil {
		return tweets, err
	}
	tweets = tweetsFromSearchResult(searchResponse)
	return tweets, nil
}

// tweetsFromSearchResult converts twitters searchResponse.Tweets into a slice of Tweet
func tweetsFromSearchResult(response searchResponse) []Tweet {
	tweets := make([]Tweet, len(response.Tweets))

	for i, tweet := range response.Tweets {
		tweets[i] = convertToTweet(tweet, response.Includes, nil)
	}
	return tweets
}

// convertToTweet merges twitters tweet and includes metadata
// into a Tweet object
func convertToTweet(tweet tweet, incl includes, matches *[]StreamRule) Tweet {
	var author Author

	for _, user := range incl.Users {
		if user.ID == tweet.AuthorID {
			author.Name = user.Name
			author.Handle = user.Handle
			author.Picture = user.Picture
			author.Verified = user.Verified
			author.ID = user.ID
			break
		}
	}

	var images []string
	var hasVideo bool
	var videoPreview string
	for _, mediaKey := range tweet.Attachments.MediaKeys {
		for _, mediaItem := range incl.Media {
			if mediaKey == mediaItem.MediaKey {

				switch mediaItem.Type {
				case "photo":
					images = append(images, mediaItem.URL)
				case "video":
					hasVideo = true
					videoPreview = mediaItem.VideoPreviewImageURL
				}
			}
		}
	}

	var pollOptions []PollOption
	for _, pollID := range tweet.Attachments.PollIDs {
		for _, poll := range incl.Polls {
			if poll.ID == pollID {
				pollOptions = poll.Options
			}
		}
	}

	var rules []string
	if matches != nil {
		rules = make([]string, len(*matches))
		for i, rule := range *matches {
			rules[i] = rule.ID
		}
	}
	return Tweet{
		ID:              tweet.ID,
		Text:            tweet.Text,
		Author:          author,
		Created:         tweet.CreatedAt,
		Images:          images,
		Retweets:        tweet.Metrics.Retweets,
		Replies:         tweet.Metrics.Replies,
		Likes:           tweet.Metrics.Likes,
		Quotes:          tweet.Metrics.Quotes,
		RuleIDs:         rules,
		HasVideo:        hasVideo,
		VideoPreviewURL: videoPreview,
		Sensitive:       tweet.Sensitive,
		Poll:            pollOptions,
	}
}
