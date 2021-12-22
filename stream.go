package twitter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
)

// streamResponse represents the data returned by twitters stream api
type streamResponse struct {
	Tweet    tweet        `json:"data"`
	Includes includes     `json:"includes"`
	Matches  []StreamRule `json:"matching_rules"`
}

// for some reason, the rule ID used by Twitter is sometimes a string and sometimes an int ¯\_(ツ)_/¯

/*type streamRuleInt struct {
	ID   int    `json:",omitempty"`
	Rule string `json:"value"`
}*/

// streamRuleResponse represents the response of twitters /2/tweets/search/stream/rules endpoint
type streamRuleResponse struct {
	Rules []StreamRule `json:"data"`
	Meta  struct {
		Summary struct {
			Created    int
			NotCreated int `json:"not_created"`
		}
	}
	Errors []StreamRule // the error contains info about a duplicated rule
}

// StreamRule defines which tweets the filtered stream should return
type StreamRule struct {
	ID   string `json:",omitempty"`
	Rule string `json:"value"`
}

// StreamSubscription contains a channel Tweets which receives Tweets that match a Rule
type StreamSubscription struct {
	Tweets chan Tweet
	Rule   StreamRule
}

// SubscribeStream returns a StreamSubscription that holds a channel which allows receiving streamed tweets
func (tw *Client) SubscribeStream(rule StreamRule) StreamSubscription {
	tw.Lock()
	defer tw.Unlock()

	results := make(chan Tweet)

	sub := StreamSubscription{results, rule}
	tw.streamSubscribers = append(tw.streamSubscribers, sub)

	return sub
}

// UnsubscribeStream removes the subscriber from the streamSubscribers slice and
// closes their channels
func (tw *Client) UnsubscribeStream(subToRemove StreamSubscription) {
	tw.Lock()
	defer tw.Unlock()

	index := -1
	ruleIsOrphaned := true
	for i, sub := range tw.streamSubscribers {
		if subToRemove == sub {
			index = i
		} else if subToRemove.Rule.ID == sub.Rule.ID {
			ruleIsOrphaned = false
		}
		if index != -1 && !ruleIsOrphaned {
			break
		}
	}
	if index != -1 {
		tw.streamSubscribers = append(tw.streamSubscribers[:index], tw.streamSubscribers[index+1:]...)
	}

	go func() {
		if ruleIsOrphaned {
			tw.logger.Println("removing orphaned rule ", subToRemove.Rule)
			err := tw.DeleteStreamRule(subToRemove.Rule)
			if err != nil {
				tw.logger.Printf("Failed to remove orphaned rule: %s", err)
			}
		} else {
			tw.logger.Println("keeping rule ", subToRemove.Rule)
		}
	}()

	if len(tw.streamSubscribers) == 0 {
		tw.logger.Println("no subs left, sending stop")
		tw.stopStreamChan <- true
	}
	close(subToRemove.Tweets)
}

// StartStream begins to stream tweets if the Client is not already streaming
func (tw *Client) StartStream() {
	tw.Lock()
	defer tw.Unlock()

	if !tw.streaming {
		tw.streaming = true
		tw.logger.Println("starting stream")
		go tw.stream()
	}
}

// StopStream send a signal to stream() to stop streaming
func (tw *Client) StopStream() {
	tw.stopStreamChan <- true
}

// stream connects to twitters /2/tweets/search/stream and retrieves Tweets matching predefined rules.
// Results are sent to all subscribers in the Clients streamSubscribers slice.
// When no subscribers are left, streaming is ended
func (tw *Client) stream() {

	tw.logger.Println("Stream()")
	defer tw.logger.Println("stop streaming")
	defer func() { tw.streaming = false }()

	reqURL := fmt.Sprintf("%s/tweets/search/stream?%s", apiRoot, expansionsAndFields)

	ctx, cancelRequest := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	defer cancelRequest()
	if err != nil {
		tw.logger.Printf("failed to build request %s", err)
		return
	}

	resp, err := tw.authenticatedTwitterRequest(req)
	if err != nil {
		tw.logger.Printf("failed to fetch stream %s", err)
		return
	}
	defer resp.Body.Close()

	decoder := json.NewDecoder(resp.Body)

	// decode incoming tweets in the background and send them into a channel
	tweetChan := make(chan Tweet)
	errChan := make(chan error)
	go func() {
		for decoder.More() {
			var result streamResponse

			err := decoder.Decode(&result)
			if err != nil {
				errChan <- err
			}
			tweet := convertToTweet(result.Tweet, result.Includes, &result.Matches)

			if tw.EnableAllTweetsChannel {
				tw.StreamedTweets <- tweet
			}
			tweetChan <- tweet
		}
		close(tweetChan)
	}()

	// forward decoded tweets to the subscribers
	go func() {
		for tweet := range tweetChan {
			tw.Lock()
			for _, sub := range tw.streamSubscribers {
				for _, match := range tweet.RuleIDs {
					if match == sub.Rule.ID {
						sub.Tweets <- tweet
					}
				}
			}
			tw.Unlock()
		}
	}()

	// exit when there's an error or no subscriber left
	// or something goes wrong
	select {
	case err = <-errChan:
		tw.logger.Println("[Stream] got error, exiting: ", err)
	case <-tw.stopStreamChan:
		tw.logger.Println("[Stream] got stop signal, exiting...")
	}
}

// GetStreamRules calls the Twitter api and returns all rules for the stream
func (tw *Client) GetStreamRules() (rules []StreamRule, err error) {
	reqURL := fmt.Sprintf("%s/tweets/search/stream/rules", apiRoot)

	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return
	}

	result, err := tw.authenticatedTwitterRequest(req)
	if err != nil {
		return
	}
	defer result.Body.Close()

	var streamRuleResponse streamRuleResponse
	err = json.NewDecoder(result.Body).Decode(&streamRuleResponse)

	return streamRuleResponse.Rules, err
}

// CreateStreamRule creates a new rule for the streaming endpoint.
// options accepts strings for keywords and options like ImageFilter.
// If the rule already exists, err is nil and the rule is returned
func (tw *Client) CreateStreamRule(options ...string) (rule StreamRule, err error) {
	reqURL := fmt.Sprintf("%s/tweets/search/stream/rules", apiRoot)

	ruleBuilder := strings.Builder{}

	for _, option := range options {
		ruleBuilder.WriteString(" ")
		ruleBuilder.WriteString(option)
	}

	rule = StreamRule{
		Rule: ruleBuilder.String(),
	}
	reqBody := make(map[string][]StreamRule)
	reqBody["add"] = []StreamRule{
		rule,
	}
	reqBodyJSON, err := json.Marshal(&reqBody)

	req, err := http.NewRequest(http.MethodPost, reqURL, bytes.NewReader(reqBodyJSON))
	if err != nil {
		return
	}
	req.Header.Add("Content-Type", "application/json")

	result, err := tw.authenticatedTwitterRequest(req)
	if err != nil {
		return
	}
	defer result.Body.Close()

	if result.StatusCode != http.StatusCreated {
		return rule, errors.New("failed to create rule")
	}

	var streamRuleResponse streamRuleResponse
	err = json.NewDecoder(result.Body).Decode(&streamRuleResponse)
	if err != nil {
		tw.logger.Printf("Failed to Unmarshal json: %s", err)
		return
	}

	// if the response is StatusCreated but no rule was created, it already exists
	if streamRuleResponse.Meta.Summary.Created != 1 && len(streamRuleResponse.Errors) > 0 {
		rule = streamRuleResponse.Errors[0]
	} else {
		rule = streamRuleResponse.Rules[0]
	}
	return
}

// DeleteStreamRule calls the Twitter api to remove a rule from the stream
func (tw *Client) DeleteStreamRule(rule StreamRule) (err error) {
	return tw.DeleteStreamRules([]StreamRule{rule})
}

// DeleteStreamRules calls the Twitter api to remove a set of rules from the stream
func (tw *Client) DeleteStreamRules(rules []StreamRule) (err error) {
	reqURL := fmt.Sprintf("%s/tweets/search/stream/rules", apiRoot)

	type DeleteRequestBody struct {
		Delete struct {
			Ids []string `json:"ids"`
		} `json:"delete"`
	}

	var reqBody DeleteRequestBody
	reqBody.Delete.Ids = make([]string, len(rules))
	for i, rule := range rules {
		reqBody.Delete.Ids[i] = rule.ID
	}

	reqBodyJSON, err := json.Marshal(&reqBody)

	req, err := http.NewRequest(http.MethodPost, reqURL, bytes.NewReader(reqBodyJSON))
	if err != nil {
		return
	}
	req.Header.Add("Content-Type", "application/json")

	result, err := tw.authenticatedTwitterRequest(req)
	if err != nil {
		return
	}
	defer result.Body.Close()
	data, err := ioutil.ReadAll(result.Body)
	if err != nil {
		return
	}

	if result.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to delete rule. Response: %s", string(data))
	}

	return nil
}
