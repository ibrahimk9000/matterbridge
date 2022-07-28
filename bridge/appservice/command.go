package bappservice

import "github.com/42wim/matterbridge/bridge/config"

func (b *AppServMatrix) ControllAction(msg config.Message) {

	switch msg.Event {
	case "join":
		b.Remote <- config.Message{
			Text:     "join",
			Channel:  msg.ChannelName,
			Username: b.Name,
			UserID:   b.UserID,
			Account:  b.Account,
			Event:    "join",
			Protocol: "appservice",
			ExtraNetworkInfo: config.ExtraNetworkInfo{
				ActionCommand:  msg.Account,
				TargetPlatform: b.RemoteProtocol,
			},
		}
	}

	// TODO
	// join
	// send res back to the api

}
