package bappservice

import (
	"encoding/json"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/42wim/matterbridge/bridge"
	"github.com/42wim/matterbridge/bridge/config"
	matrix "github.com/matterbridge/gomatrix"
	gomatrix "maunium.net/go/mautrix"
)

func TestAppServMatrix_Connect(t *testing.T) {
	type fields struct {
		mc          *matrix.Client
		apsCli      *gomatrix.Client
		UserID      string
		NicknameMap map[string]NicknameCacheEntry
		RoomMap     map[string]string
		roomsInfo   map[string]MatrixRoomInfo
		rateMutex   sync.RWMutex
		RWMutex     sync.RWMutex
		Config      *bridge.Config
	}
	b := AppServMatrix{
		mc:          &matrix.Client{},
		apsCli:      &gomatrix.Client{},
		UserID:      "",
		NicknameMap: map[string]NicknameCacheEntry{},
		RoomMap:     map[string]string{},
		roomsInfo:   map[string]*MatrixRoomInfo{},
		rateMutex:   sync.RWMutex{},
		RWMutex:     sync.RWMutex{},
		Config:      &bridge.Config{},
	}
	b.Connect()

	channelMember := []string{"kof", "meta", "woi"}

	msg := config.Message{
		Text:             "hi good morning",
		Channel:          "#prog",
		Username:         "exterUser",
		UserID:           "123456",
		Avatar:           "",
		Account:          "exterUserAccount",
		Event:            "new_users",
		Protocol:         "",
		Gateway:          "",
		ParentID:         "",
		Timestamp:        time.Time{},
		ID:               "147852",
		Extra:            map[string][]interface{}{},
		ExtraNetworkInfo: config.ExtraNetworkInfo{
			ChannelUsersMember: channelMember,
			ActionCommand:      "",
			ChannelId:          "",
		},
	}
	b.LoadState()

	b.Send(msg)
	b.SaveState()
	time.Sleep(4 * time.Minute)
}
func TestAppServMatrix_Event(t *testing.T) {
	evBody := `{
	"events":[
	   {
		  "content":{
			 "displayname":"_irc_bridge_ex",
			 "membership":"invite"
		  },
		  "event_id":"$X4P4qoJV9ol9xl36C-IBMAw2EsAUziVLpp7nO9hUr-g",
		  "origin_server_ts":1656724456554,
		  "room_id":"!Je5iMpaE2o95pw6u:localhost",
		  "sender":"@sampletest:localhost",
		  "state_key":"@_irc_bridge_ex:localhost",
		  "type":"m.room.member",
		  "unsigned":{
			 "age":43,
			 "invite_room_state":[
				{
				   "content":{
					  "displayname":"_irc_bridge_ex",
					  "membership":"invite"
				   },
				   "sender":"@sampletest:localhost",
				   "state_key":"@_irc_bridge_ex:localhost",
				   "type":"m.room.member"
				},
				{
				   "content":{
					  "creator":"@sampletest:localhost",
					  "room_version":"6"
				   },
				   "sender":"@sampletest:localhost",
				   "state_key":"",
				   "type":"m.room.create"
				},
				{
				   "content":{
					  "join_rule":"public"
				   },
				   "sender":"@sampletest:localhost",
				   "state_key":"",
				   "type":"m.room.join_rules"
				},
				{
				   "content":{
					  "name":"kkko"
				   },
				   "sender":"@sampletest:localhost",
				   "state_key":"",
				   "type":"m.room.name"
				},
				{
				   "content":{
					  "alias":"#kko:localhost"
				   },
				   "sender":"@sampletest:localhost",
				   "state_key":"",
				   "type":"m.room.canonical_alias"
				},
				{
				   "content":{
					  "displayname":"_irc_bridge_ex",
					  "membership":"invite"
				   },
				   "sender":"@sampletest:localhost",
				   "state_key":"@_irc_bridge_ex:localhost",
				   "type":"m.room.member"
				}
			 ]
		  }
	   }
	]
 }`
	type AppEvents struct {
		Events []*matrix.Event `json:"events,omitempty"`
	}
	var apsEvents AppEvents

	err := json.Unmarshal([]byte(evBody), &apsEvents)
	log.Println(string(evBody))
	if err != nil {
		log.Println(err)
		return
	}
	log.Println(apsEvents)
}



