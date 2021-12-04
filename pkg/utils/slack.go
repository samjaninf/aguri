package utils

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/slack-go/slack"
	"github.com/whywaita/aguri/pkg/config"
	"github.com/whywaita/aguri/pkg/store"
)

var (
	reUser = regexp.MustCompile(`<@U(\S+)>`)
)

func IsExistChannel(api *slack.Client, searchName string) (bool, *slack.Channel, error) {
	// channel is exist => True
	targetConversationsType := []string{"public_channel", "private_channel"}
	param := &slack.GetConversationsParameters{
		Types: targetConversationsType,
	}

	channels, cursor, err := api.GetConversations(param)
	if err != nil {
		return false, nil, err
	}

	for _, channel := range channels {
		if channel.Name == searchName {
			return true, &channel, nil
		}
	}

	for cursor != "" {
		// exists next pages
		param := &slack.GetConversationsParameters{
			Cursor: cursor,
			Types:  targetConversationsType,
		}
		cs, cur, err := api.GetConversations(param)
		if err != nil {
			return false, nil, err
		}

		for _, c := range cs {
			if c.Name == searchName {
				return true, &c, nil
			}
		}

		cursor = cur
	}

	// not found
	return false, nil, fmt.Errorf("%s is not found", searchName)
}

func CreateNewChannel(api *slack.Client, name string) error {
	var err error
	_, err = api.CreateConversation(name, false)
	if err != nil {
		return fmt.Errorf("failed to create new channel: %w", err)
	}

	return nil
}

func GetMessageByTS(api *slack.Client, channel, timestamp string) (*slack.Message, error) {
	// get message via RestAPI by Timestamp
	// want to get only one message
	historyParam := &slack.GetConversationHistoryParameters{
		ChannelID: channel,
		Latest:    timestamp,
		Oldest:    timestamp,
	}

	history, err := api.GetConversationHistory(historyParam)
	if err != nil {
		return nil, fmt.Errorf("failed to get message history by timestamp: %w", err)
	}

	msg := history.Messages[0]

	return &msg, nil
}

func ConvertId2NameInMsg(msg string, ev *slack.MessageEvent, fromAPI *slack.Client) (string, error) {
	userIds := reUser.FindAllStringSubmatch(ev.Text, -1)
	if len(userIds) != 0 {
		for _, ids := range userIds {
			id := strings.TrimPrefix(ids[0], "<@")
			id = strings.TrimSuffix(id, ">")
			name, _, err := ConvertDisplayUserName(fromAPI, ev, id)
			if err != nil {
				return "", err
			}
			msg = strings.Replace(msg, id, name, -1)
		}
	}

	return msg, nil
}

func GetUserInfo(fromAPI *slack.Client, ev *slack.MessageEvent) (username, icon string, err error) {
	// get source username and channel, im, group
	user, usertype, err := ConvertDisplayUserName(fromAPI, ev, "")
	if err != nil {
		return "", "", fmt.Errorf("failed to convert display name: %w", err)
	}

	if usertype == "user" {
		u, err := fromAPI.GetUserInfo(ev.Msg.User)
		if err != nil {
			return "", "", fmt.Errorf("failed to get user info: %w", err)
		}
		icon = u.Profile.Image192
	} else {
		icon = ""
	}

	return user, icon, nil
}

func PostMessageToChannel(toAPI, fromAPI *slack.Client, ev *slack.MessageEvent, msg, aggrChannelName string) error {
	// post aggregate message
	var err error

	isExist, _, err := IsExistChannel(toAPI, aggrChannelName)
	if isExist == false {
		return fmt.Errorf("channel is not found: %w", err)
	}
	if err != nil {
		return fmt.Errorf("failed to get info of exist channel: %w", err)
	}

	user, icon, err := GetUserInfo(fromAPI, ev)
	fType, position, err := ConvertDisplayChannelName(fromAPI, ev)
	if err != nil {
		return fmt.Errorf("failed to convert channel name: %w", err)
	}

	param := slack.PostMessageParameters{
		IconURL: icon,
	}
	username := user + "@" + strings.ToLower(fType[:1]) + ":" + position
	param.Username = username

	attachments := ev.Attachments

	// convert user id to user name in message
	msg, err = ConvertId2NameInMsg(msg, ev, fromAPI)
	if err != nil {
		return fmt.Errorf("failed to convert id to name: %w", err)
	}

	workspace := strings.TrimPrefix(aggrChannelName, config.PrefixSlackChannel)
	if msg != "" {
		respChannel, respTimestamp, err := toAPI.PostMessage(aggrChannelName, slack.MsgOptionText(msg, false), slack.MsgOptionPostMessageParameters(param))
		if err != nil {
			return fmt.Errorf("failed to post message: %w", err)
		}
		store.SetSlackLog(workspace, ev.Timestamp, position, msg, respChannel, respTimestamp)
	}
	// if msg is blank, maybe bot_message (for example, twitter integration).
	// so, must post blank msg if this post have attachments.
	if attachments != nil {
		for _, attachment := range attachments {
			respChannel, respTimestamp, err := toAPI.PostMessage(aggrChannelName, slack.MsgOptionPostMessageParameters(param), slack.MsgOptionAttachments(attachment))
			if err != nil {
				return fmt.Errorf("failed to post message: %w", err)
			}
			store.SetSlackLog(workspace, ev.Timestamp, position, msg, respChannel, respTimestamp)
		}
	}

	return nil
}

func GenerateAguriUsername(msg *slack.Message, ch *slack.Channel, displayUsername string) string {
	return displayUsername + "@" + strings.ToLower(ch.ID[:1]) + ":" + ch.Name
}

func GetAllConversations(api *slack.Client) ([]slack.Channel, error) {
	params := &slack.GetConversationsParameters{
		Cursor:          "",
		ExcludeArchived: true,
		Limit:           0,
		Types:           nil,
	}

	var channels []slack.Channel

	for {
		chs, cursor, err := api.GetConversations(params)
		if err != nil {
			return nil, fmt.Errorf("failed to get channels: %w", err)
		}
		channels = append(channels, chs...)

		if cursor == "" {
			break
		}
		params.Cursor = cursor
	}

	return channels, nil
}
